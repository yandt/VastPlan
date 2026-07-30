# ADR-0174 Portal 可选版本控制与发布快照分离

- 状态：已采纳，P2.3 待实施
- 日期：2026-07-31
- 修订：[ADR-0171](ADR-0171-Portal单聚合版本与上线子流程.md) 中“Portal 天然以 PortalVersion 为工作对象”的部分、[ADR-0172](ADR-0172-通用版本账本与可插拔存储Provider.md) 与 [ADR-0173](ADR-0173-版本环境与资源适配.md) 中“Portal 必然迁入版本账本”的部分
- 关联：[ADR-0142](ADR-0142-内核启动与业务发布完全分离.md)、[ADR-0173](ADR-0173-版本环境与资源适配.md)

## 背景

Portal 当前把可编辑草稿、审批候选、内容版本和上线来源都放进 `PortalVersion`。这保证了发布安全，但也意味着即使企业没有配置 Version Workspace，系统仍在隐式维护完整版本历史，违背“普通目录只有显式接入版本控制后才出现 commit/history/diff”的产品语义。

发布安全又不能依赖版本控制：即使没有 Git、Ledger 或 Workspace，审批内容也必须在发布期间冻结，线上 Release 也必须可复现、可审计和可回滚。因此需要把“版本控制”与“发布快照”拆开，而不是在未配置时偷偷提供一个 Local Version Backend。

## 决策

1. `Portal` 继续是唯一在线治理聚合，但内部拆为可变 `WorkingCopy`、不可变 `Publication`、运行态 `PortalRelease` 和可选 `VersionControlBinding`。
2. 未配置 `VersionControlBinding` 就表示没有版本管理。系统不创建本地版本、VersionRef、Head 或隐藏历史，也不调用 Workspace/Ledger。
3. `WorkingCopy.revision` 只是 Shared State CAS 令牌，不是业务版本号。保存只覆盖当前工作副本，不产生历史。
4. `Publication` 是审批和发布所需的冻结候选，不是通用版本记录。无版本控制时它内联规范化配置与 digest；启用 Workspace 时它引用精确 VersionRef，并保留当前审批/发布热投影。
5. `PortalRelease` 在两种模式下都存在。它引用一个精确 Publication，保存物化 PortalSpec、制品引用、前一 Release 和阶段结果；Release 历史属于部署审计，不等于工作副本版本历史。
6. Workspace 是可选版本控制能力。Portal Composer 对其声明 soft/degrade 依赖；未配置时 Portal 不降级，已配置但能力不可用时只阻止该 Portal 的版本提交、历史详情和比较，不得静默退回无版本模式。
7. P2.3 最小版本粒度为“提交审批时 commit”。普通保存不产生版本；手工 checkpoint、branch、merge 等交互以后按真实需求增加。
8. Portal 只通过中立 `PortalVersionControl` 端口调用 Workspace，不直调具体插件或 Ledger Provider。浏览器不能选择插件 ID、Provider、namespace、endpoint 或凭证。
9. Workspace commit 在 Portal 接入时创建 detached VersionRef，不提前移动 Head。Portal 聚合 CAS 成功后 VersionRef 才成为领域事实；P2.3 不要求 Head 镜像，也不为它引入跨插件事务。
10. 当前开发数据不迁移。P2.3 提升 Portal Composer 状态格式和能力主版本，清理旧开发状态后重新生成；未来真实生产数据的 attach/detach 必须另行设计显式迁移控制器。

## 状态模型

```text
WorkingCopy: Mutable ──submit──> Publication: PendingApproval → Approved → Published

PortalRelease: Preparing → Current → Superseded
                           └───────→ Failed
```

启用 Workspace 时，`submit` 在生成 Publication 前提交不可变 VersionRef；未启用时，`submit` 冻结当前配置和 digest。两条路径进入同一个审批、发布、物化、上线和回滚状态机。存在 PendingApproval 或 Approved 候选时 WorkingCopy 暂停修改，Publication 进入 Published 后才允许继续编辑下一批变化。

## 备选方案

- **保留 Local/Workspace 两种 Version Backend**：未配置时仍有隐藏版本历史，不符合产品语义，拒绝。
- **完全取消无版本模式的冻结快照和 Release 历史**：审批内容可在批准后被修改，线上状态无法复现，拒绝。
- **Portal 与 Workspace 双写完整历史**：需要对账、补偿和冲突裁决，形成两个真相源，拒绝。
- **配置 Workspace 后故障时自动退回无版本模式**：会让同一 Portal 产生断裂历史和不可证明的发布来源，拒绝。

## 影响

正面：未启用版本控制时产品模型和运行负担都最小；启用后获得统一历史、diff 和恢复；发布安全不依赖 Workspace 可用性；Portal Runtime 继续只消费当前 Release 热快照。

代价：当前 `PortalVersion` 聚合需要在 P2.3 拆分；BFF/Workbench API 与权限操作要同步升级；Workspace 需先补稳定 operation ID 和已提交版本读取/比较两个窄能力。
