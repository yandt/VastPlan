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

## 2026-07-28 修订：区分业务 revision 与聚合文档 CAS

PortalPreference 的单个 scope revision 与 `tenant + subject` 聚合文档 revision 属于两层并发控制，不再映射为同一种冲突：

1. 目标 scope 的 `ExpectedRevision` 不匹配仍返回 `portal.preference.conflict`，表示同一偏好确实已由其他会话更新。
2. Shared State 聚合文档 CAS 冲突由服务端有界重载最新文档、重新校验目标 scope revision 并合并写入；不同 scope 的并发更新不再产生虚假的“其他设备更新”。
3. 重载后若目标 scope revision 已变化，立即返回真实业务冲突；若持续发生聚合文档争用，则按存储不可用处理，不伪装成业务 revision 冲突。
4. 回归测试同时覆盖“不同 scope 自动重基后均保留”和“同一 scope 仍保持 CAS 单赢家”。

## 2026-07-30 修订：组合治理 Root CAS 与内容寻址快照

原“每 tenant 单文档”在 PortalVersion、Release、Test Release 与审计历史增长后会触达 Shared State 的 1 MiB 单值上限，故升级为仍保持单提交点的分块快照：

1. `tenant` key 改存带 format、完整大小、完整 SHA-256 和 chunk 清单的小型 Root；Root revision 继续作为治理聚合唯一 CAS fence。
2. 完整状态序列化后按 512 KiB 切分，以 `blob/<sha256>` 不可变写入。全部 chunk 完成且复核冲突内容一致后才 CAS Root，因此读者只会看到旧完整快照或新完整快照。
3. 读取必须逐块复核 key、大小、chunk 摘要和完整快照摘要，任一缺失或篡改均 fail-closed。当前快照安全上限为 64 MiB，Root 本身仍远低于单值上限。
4. 旧版直接存放在 `tenant` key 的 JSON 文档继续严格可读；下一次成功治理写使用同一 key revision 原位提交 Root，不要求停机迁移或双写。
5. Frontend Test Release 的候选归属与候选版本在同一次 Root CAS 中提交。内部 `TestVersionOwners` 把测试候选排除于正式 PortalVersion、PortalRelease、普通发布和普通回滚入口；测试 Runtime Activation 仍由专用 Test Release 状态机管理。

该修订不改变 active-active、tenant scope、状态机或浏览器契约，也不把跨 key 事务伪装成原子操作：chunk 是提交前不可变准备数据，只有 Root CAS 决定可达快照。失败 CAS 可能留下不可达 chunk，后续应沿用 Credentials 的“宽限期 + Root 二次复核”方式做独立清理，清理能力不得进入写事务关键路径。
