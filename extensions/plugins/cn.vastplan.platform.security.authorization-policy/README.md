# Authorization Policy

`cn.vastplan.platform.security.authorization-policy` 是平台级授权真相源。它以 leader 模式管理 Permission Catalog、版本化 Role、Subject Binding、撤权序列和 Ed25519 签名 Policy Snapshot。

Role 与 Binding 均使用 `Draft → PendingApproval → Approved → Published → Retired`，创建人与审批人必须不同。所有写操作携带 `expectedGeneration`，由文件 Store 以 CAS 拒绝并发覆盖。`revoke` 会在一次服务端流程中递增撤权 revision 并发布新签名快照，调用成功即表示本地 Enforcer 已有可消费的新撤权材料。

运行时需要以下宿主允许的环境变量：

- `VASTPLAN_AUTHORIZATION_PERMISSION_CATALOG`
- `VASTPLAN_AUTHORIZATION_POLICY_STATE`
- `VASTPLAN_AUTHORIZATION_POLICY_KEY`
- `VASTPLAN_AUTHORIZATION_POLICY_SNAPSHOT`
- `VASTPLAN_AUTHORIZATION_POLICY_AUDIENCE`

本插件不提供登录或目录同步。它同时交付 Backend 授权治理服务和 Portal Workbench 管理页面，使角色、绑定、撤权、审计与签名 Policy Snapshot 共用一个插件身份和版本；最终判定仍由每内核的 `authorization-enforcer` 执行。
