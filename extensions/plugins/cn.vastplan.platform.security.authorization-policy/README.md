# Authorization Policy

`cn.vastplan.platform.security.authorization-policy` 是平台级授权真相源。它以 `leader + external-shared` 模式管理 Permission Catalog、版本化 Role、Subject Binding、撤权序列和 Ed25519 签名 Policy Snapshot。运行期状态通过可信宿主的 fenced Shared State 保存；插件不直接连接 NATS，也不持有物理 bucket 名或凭据。

Role 与 Binding 均使用 `Draft → PendingApproval → Approved → Published → Retired`，创建人与审批人必须不同。所有写操作携带 `expectedGeneration`，由文件 Store 以 CAS 拒绝并发覆盖。`revoke` 会在一次服务端流程中递增撤权 revision 并发布新签名快照，调用成功即表示本地 Enforcer 已有可消费的新撤权材料。

Role 的权限输入支持精确权限码和受限 Glob。`*` 只匹配一个点分段，`**` 匹配一个或多个点分段，且首段必须是字面命名空间。服务在保存 Role revision 时将选择器与当前签名 Permission Catalog、Domain 委托上限、风险上限和 `assignable` 属性求交，保存原始选择器、Catalog digest 与确定排序的精确权限集；发布快照和运行期判定只消费精确权限码。Catalog 后续新增权限不会静默扩大既有 Role revision。

运行时需要以下宿主允许的环境变量：

- `VASTPLAN_AUTHORIZATION_PERMISSION_CATALOG`
- `VASTPLAN_AUTHORIZATION_POLICY_BOOTSTRAP_STATE`（可选，仅在 Shared State 尚无权威对象时首次导入；不可作为故障回退）
- `VASTPLAN_AUTHORIZATION_POLICY_BOOTSTRAP_RECONCILIATION`（可选；只有可信组合根执行显式 bootstrap 时可设为 `seed-owned`，普通启动必须为空）
- `VASTPLAN_AUTHORIZATION_POLICY_KEY`
- `VASTPLAN_AUTHORIZATION_POLICY_SNAPSHOT`
- `VASTPLAN_AUTHORIZATION_POLICY_AUDIENCE`

本插件不提供登录或目录同步。它同时交付 Backend 授权治理服务和 Portal Workbench 管理页面，使角色、绑定、撤权、审计与签名 Policy Snapshot 共用一个插件身份和版本；最终判定仍由每内核的 `authorization-enforcer` 执行。
