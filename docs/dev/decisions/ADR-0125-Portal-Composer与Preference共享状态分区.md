# ADR-0125 Portal Composer 与 Preference 共享状态分区

- 状态：已采纳并实现
- 日期：2026-07-23

## 背景

Portal Composer 旧版在同一 leader 进程中维护两个本地文件：一个保存 Profile、Application、Binding、Activation、Frontend Test Release、引用 outbox 和审计；另一个保存所有租户与用户的 PortalPreference。直接把两个文件各自变成单个 Shared State value，会继续保留跨租户耦合，并使活跃企业的用户偏好很快超过 1 MiB。

## 决策

1. 继续使用 Go 并运行在既有可信插件进程。Portal 治理状态机、Catalog/制品引用协议和 BFF 契约均已在 Go；Node/Python 不提供足以抵消重写风险的生态优势。
2. 组合治理状态按 tenant 保存为 `tenant/portal.composition/tenant` 单文档。该文档包含该租户的 Application、Binding、Activation、Test Release、outbox 与审计，以及可引用的签名 Platform Profile Seed；一次治理转换仍是一个 CAS 提交。
3. Platform Catalog 继续由 `kernel.config.get` 提供，Shared State 不保存第二份 Catalog 真源。租户文档首次打开或 Catalog generation 更新时，只把签名 Profile 与该 tenant 的 Binding 投影为已发布 Seed。
4. PortalPreference 按 `tenant + subject` 分文档，namespace 为 `portal.preferences`，key 为 subject SHA-256 摘要。每个用户文档最多保存 64 个 Portal/Catalog scope 与有界审计；用户之间不争用同一个 CAS，也不在 key 中暴露 subject。
5. 两类状态都只申请 Shared State get/create/update。插件改为 `active-active + external-shared + queue`；Provider 不可用时 fail-closed，不回退本地文件。
6. 组合治理 stale writer 返回可重试 `portal.composer.conflict`；偏好业务 revision 冲突与 Store CAS 冲突统一返回稳定 `portal.preference.conflict`，客户端继续执行一次重新读取与合并。
7. Shared State 解码严格拒绝未知字段与尾随 JSON；租户聚合加载时验证所有 Application、Binding、Activation、Test Release 和审计的 tenant，只有签名 Platform Profile 允许 `tenant="*"`。
8. 当前为开发阶段，不迁移旧 `platform.portal-composer.stateFile` 与 `preferenceStateFile`；生产历史形成后必须设计在线导入、双读核对和可回滚切换。

## 影响

正面：Portal 治理和用户偏好均可跨节点恢复；用户偏好随用户数水平分片；跨租户数据在物理 scope 与文档校验两层隔离；Portal Composer 不再依赖共享卷。

代价：同一租户的治理写仍共享一个 CAS；大型租户治理历史接近 1 MiB 时必须进入“根指针 + 分片 Saga”升级；Platform Catalog 更新会在租户首次访问时产生一次 Seed CAS。

## 验证

- 原有异人审批、Activation、精确回滚、引用 outbox、Test Release 恢复与 Preference CAS 测试继续通过。
- 新测试覆盖两个 Composer 实例共享治理状态、tenant 隔离、两个 Preference 实例共享用户状态、subject 隔离和 Store CAS 单赢家。
