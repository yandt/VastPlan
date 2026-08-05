# ADR-0195 插件升级前 DataModel 迁移门禁

- 状态：已采纳并实施
- 日期：2026-08-05
- 关联：[ADR-0191](ADR-0191-统一插件安装意图与多入口生命周期控制.md)、[ADR-0193](ADR-0193-第一方插件边界与Capability契约版本.md)、[ADR-0194](ADR-0194-平台控制数据库与声明式数据分层.md)

## 背景

Record Store 已具备 DataModel、签名迁移、数据库锁和持久迁移账本，但插件安装候选此前只展示制品与配置变化，发布链没有把 DataModel 差异、人工确认和 Node Agent 候选切换组成一个事务边界。这样会造成两个风险：用户批准插件版本时不知道需要迁移；候选进程可能在 Schema 尚未准备时进入路由。

## 决策

1. 内核预览从精确 Artifact Lock 投影 SQL-free `DataModelCatalog`，并随 Service Revision 持久化。Catalog 包含 owner、Artifact SHA、完整 DataModel、精确存储绑定和签名迁移身份，不包含 SQL、密码或驱动。
2. 插件安装预览比较活动与候选 Catalog，统一分类为 `create/additive/signed/manual/retained`。新建表、可空字段和非唯一索引是安全变化；删除、改类型、收窄约束和数据重写必须命中同 owner、精确 from/to 版本的签名迁移；未命中时候选直接拒绝。
3. 生产候选必须先审批。审批绑定 Schema Impact digest；签名迁移还必须提交 `database.backup-ref`，由当前 Approval Policy 求值并只持久化不透明证据引用。开发候选只能自动执行安全变化，禁止自动授权签名迁移。
4. `foundation.data.record-store@1.2.0` 增加宿主专属 `SchemaActivation`。Deployment Publisher 只接受认证 Deployment Manager 注入该字段，并把它写入受签名制品和 Deployment digest 共同约束的 Resolution；普通插件配置不能写入或覆盖。
5. Node Agent 固定执行顺序为：启动候选插件、同步同代可信 DataModel Inventory、再次调用 Runtime `schemaPlan`、执行已授权迁移、读取 `schemaStatus` 复核版本与摘要、处理插件私有状态迁移、注册候选路由。任一阶段失败都不提交候选路由，旧实例继续运行。
6. 多副本可同时观察同一候选，但 Schema Controller 使用数据库 advisory lock、迁移账本和幂等目标身份确保物理单写。每个节点必须得到相同计划；运行时计划与审批计划种类不一致时 fail-closed。
7. 安装激活在 Backend 发布后等待精确 revision readiness，再提交 Portal Activation。Schema 失败或候选未就绪会发布更高的单调回滚修订并保留旧 Generation；签名破坏性迁移不承诺数据库降级，回滚计划若需要降低 schemaVersion 会被拒绝并转入 forward-fix/恢复流程。
8. `connection-ref` 模型必须在插件非敏感配置的 `recordStoreBindings` 中为每个模型绑定精确 `ConnectionRef` revision。平台控制模型继续使用宿主保留连接，任何业务插件都不能选择该连接。

## 影响

- 用户批准的是“插件制品 + 配置 + 数据迁移”同一个候选，而不是批准插件后再临时发现数据库变化。
- 安全增量可以自动化，破坏性变化必须显式迁移、审批和备份证据；两者仍走同一 Schema Controller。
- 数据库迁移成功但进程切换失败时，只有向后兼容的 expand 变化可自动回到旧代；任意自动数据库 downgrade 被明确禁止。
- `DataModelCatalog`、Schema Impact 和 Runtime 账本三处摘要相互复核，不能用前端提示代替运行时门禁。

## 不采用

- **插件启动后自行迁移**：会让业务插件持有 DDL 权限并在多副本下竞争，拒绝。
- **发布成功后再迁移**：会形成新代码读取旧 Schema 的窗口，拒绝。
- **只按插件 SemVer 决定是否迁移**：插件版本、Capability 契约和 DataModel Schema 版本职责不同，拒绝。
- **自动执行 down migration**：数据库写入可能已使用新结构，自动降级会造成不可逆数据损坏，拒绝。
