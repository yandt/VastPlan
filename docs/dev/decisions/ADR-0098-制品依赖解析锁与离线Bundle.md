# ADR-0098 制品依赖解析、精确锁与离线 Bundle

- 状态：已采纳，分阶段实施
- 日期：2026-07-21
- 关联：[ADR-0023](ADR-0023-插件Schema与可验证制品仓库.md)、[ADR-0026](ADR-0026-远端制品仓库与供应链信任.md)、[ADR-0049](ADR-0049-制品信任基座与仓库基础插件.md)、[ADR-0097](ADR-0097-测试制品仓库与前端分级热升级.md)

## 背景

当前仓库已能保存不可变、已签名的精确制品，并提供 Catalog 和单调 Publish Journal，但消费方仍需自行选择版本。若 Node Agent、Portal、Runner 和 Mobile 分别实现 `latest` 和依赖选择，同一组输入会因时间、channel 或算法不同产生不同部署，气隙环境也无法重现在线解析结果。

## 决策

1. 依赖求解属于托管仓库领域插件，不进入内核。第一实现使用 Go，并运行在已有仓库 leader 进程：Go 可直接复用 SemVer、已验证 Catalog、持久化快照和签名制品适配器，无需为一次确定性求解增加 Node/Python 进程或跨语言事务。
2. Resolver 输入必须声明根约束、目标内核及版本、目标平台、允许 channel、允许发布者/命名空间和 Catalog snapshot revision。`0` 仅表示在服务端锁定当前 revision；输出必须写入实际 revision。
3. 求解使用确定性回溯，同时校验 SemVer、engine、OS/arch、发布者、channel、制品依赖和运行时 capability。版本冲突、依赖环、`strong/data` 阻塞依赖无提供者或策略越界必须 fail-closed，不返回部分锁；`soft/lazy` 留给运行时降级或延迟绑定。
4. Lockfile 是跨内核的不可变交付契约：每个包保存精确 `pluginId/version/channel`、包摘要、发布者、签名 key ID、仓库 revision 和依赖边；整份锁文件按规范 JSON 计算 SHA-256。运行节点只验证和消费锁，不重新求解。
5. 离线 Bundle 由仓库根据一份已校验 Lockfile 生成，包含锁文件、信任快照、每个精确制品原文及证明、Bundle 内容清单。导入只是新的输入适配器，仍必须经相同证明、摘要、包内清单和锁绑定校验，不得因“离线”放宽信任。
6. Resolver 可以通过带仓库读授权的 HTTPS 或 `platform.artifacts.repository` 窄 capability 调用；Bundle 大字节仅走 HTTPS，不穿过协议总线。仓库不因生成锁或 Bundle 取得部署权限。

## 备选方案

- **内核统一求解**：可强制所有运行面一致，但会把 channel、yank、替代建议和市场策略带入内核，拒绝。
- **每种内核各自解析**：开发快，但无法保证相同快照产生同一结果，拒绝。
- **立即拆独立 Resolver 微服务**：便于独立扩容，但 v1 Catalog 规模下会增加网络、快照和失败一致性成本；先保持稳定契约，未来按契约拆分。
- **使用 Node.js/Python 求解器**：两者生态强，Node.js 适合 npm 语义和 UI，Python 适合策略分析；当前契约是通用 SemVer 和已有 Go 仓库事务，引入共享 Runtime 不能带来足够收益，不作为 v1 实现。

## 影响

- 正面：Backend、Portal、Runner 和 Mobile 可共用一份精确锁；在线与气隙环境可重现同一输入。
- 正面：仓库保持内容/元数据领域，内核只执行精确验证和安装，不获得包市场逻辑。
- 代价：需维护确定性回溯、冲突诊断、锁契约和 Bundle 导入验证。
- 边界：yank/deprecate/revoke、多仓库联合求解、可选依赖和市场排名不在首个实现中，但锁文件保留状态和来源扩展位。

## 实施记录

- 2026-07-21：完成 v1 闭环。仓库插件 0.5.0 增加确定性 SemVer 回溯、Catalog revision 快照、engine/平台/发布者/命名空间策略、环与 `strong/data` capability 校验；输出经 JSON Schema 校验和规范 SHA-256 绑定的 `ArtifactLock v1`。Bundle 导出使用独立 token 并在私有临时文件中生成；导入必须重新通过本地仓库信任根、证明、字节摘要、包内清单与不可变发布。
