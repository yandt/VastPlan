# ADR-0171 Portal 单聚合版本与上线子流程

- 状态：已采纳
- 日期：2026-07-30
- 修订：[ADR-0081](ADR-0081-Portal治理与不可变Activation.md) 中 Application、Platform Profile、PortalBinding 独立治理及独立 Activation 管理面的设计
- 关联：[ADR-0142](ADR-0142-内核启动与业务发布完全分离.md)、[ADR-0165](ADR-0165-Contract-Registry与插件发布编排.md)、[ADR-0170](ADR-0170-统一Capability-Contract与可信ArtifactIdentity.md)

## 背景

旧模型要求管理员分别创建、审批和发布 Application、Platform Profile 与 PortalBinding，再选择三份 revision 创建 Activation。安全性可以成立，但一个 Portal 被拆成四个管理对象，ID、版本、上线状态和回滚入口相互独立。常见操作需要跨四张表拼装精确引用，Profile 继承和共享升级也引入了当前阶段并不需要的影响传播规则。

当前仍处于开发阶段，不承担旧治理数据兼容成本。应优先把用户模型、权限操作和持久化不变量收敛到最小完整边界。

## 决策

1. `Portal` 是唯一在线治理聚合根。同一 tenant 内 `portalId` 唯一且创建后不可修改；管理页面一行只展示一个 Portal。
2. `PortalVersion` 保存完整配置，包括原 Platform Profile、Application Composition 和 PortalBinding 服务授权。三部分只能一起创建、编辑、提交、审批和发布，不再暴露独立治理 API。
3. 每个 Portal 最多存在一个未发布候选。版本号由服务端在 Portal 内单调生成；浏览器不能指定或修改版本号。
4. Published PortalVersion 不可修改，但发布不会改变线上 Portal。
5. 上线是 Portal 子流程。`PortalRelease` 只引用一个精确 Published PortalVersion，并保存物化结果、制品引用、阶段结果、操作者和前一 Release。运行时只消费 Current Release。
6. 回滚选择历史 Release 对应的 PortalVersion，重新执行当前信任、撤销、物化、路由冲突和 CAS 校验，并创建新的 Release；不得修改历史记录。
7. 静态 Portal Platform Catalog 退化为种子/创建模板。它可以为首个 PortalVersion 提供默认平台配置和服务绑定，但不是在线治理资源，也没有独立草稿、审批或发布生命周期。
8. 内核 Recovery Baseline 保持独立。它只负责 Portal 配置损坏或无安全 Release 时提供最小可信启动与恢复入口，不能被 PortalVersion 覆盖。
9. Composer 可以在内部规范化保存平台配置、应用配置和服务绑定，但只允许单一 PortalVersion 工作流同时改变它们，并在读取时验证内部引用与状态一致。
10. 对外能力收敛为 `portalGovernance`、Portal 创建/版本变更、PortalVersion 状态迁移、Release/回滚以及测试发布。删除 Profile、Binding、Application 和 Activation 的独立浏览器路由。

## 状态机

```text
PortalVersion: Draft → PendingApproval → Approved → Published
PortalRelease: Preparing → Current → Superseded
                            └────────→ Failed
```

一个 PortalVersion 可以被多次 Release，例如历史回滚；Published 版本本身不保存“当前线上”状态。

## 影响

- 用户只需理解 Portal、版本和上线记录三个概念。
- 平台配置、功能插件和服务授权的任何变化都会形成同一个可审计版本，消除跨 revision 拼装和漂移。
- 多 Portal 批量升级平台栈不通过共享 Profile 自动传播；未来由批量工具为目标 Portal 分别生成候选版本。
- 原 Activation 的两阶段物化、CAS、制品引用保护和安全恢复机制继续作为 Release 的内部实现，不因管理模型简化而弱化。
- Shared State 使用新的 v2 namespace；开发阶段不迁移旧四领域治理数据。

## 拒绝的方案

- 只合并四个页面：底层仍要求拼装四份 revision，复杂度只是被 UI 隐藏。
- 保留共享 Profile 并允许 Portal 覆盖：需要继承、合并、影响分析和历史重放规则，超出当前需要。
- 发布即上线：无法在不改变线上状态的情况下审批和预检候选，也破坏启动与业务发布分离。

## 后续修订

2026-07-31：[ADR-0174](ADR-0174-Portal可选版本控制与发布快照分离.md) 保留本文“单一 Portal 聚合”和“上线是独立 Release 子流程”的决定，但修订 Portal 必然以 PortalVersion 为工作对象的部分。P2.3 将可变 WorkingCopy、冻结 Publication、PortalRelease 与可选 VersionControlBinding 分离；未配置 Workspace 时不生成通用版本历史。
