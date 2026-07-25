# Authorization Policy

插件 ID：`cn.vastplan.platform.security.authorization-policy`
当前制品版本：`0.2.0`

该平台全栈插件的 Backend 是在线授权唯一写入真相源，以 leader + leader-owned + cluster 运行。它消费签名制品构建的 Permission Catalog，管理 Role revision、Subject Binding revision、不同主体审批、即时撤权、审计和 Ed25519 Policy Snapshot。写入使用 generation CAS；Snapshot audience、TTL 和 Provider Profile 由可信宿主配置。

Frontend 提供权限目录、Role、Subject Binding 和审计四个 Workbench Collection 页面，只通过 Portal Management Binding 调用 `platform.authorization`，不保存授权状态、不签发快照，也不直接依赖具体 UI 框架。自 `0.2.0` 起，原 `cn.vastplan.platform.configuration.role-management` 已按 ADR-0152 并入本插件，使管理 UI 与对应 Backend 协议共用插件身份、版本和发布事务。

首次安装的 `platform.owner` 不是 `is_admin/platform.admin` 旁路，而是由 Seed Authority 物化的 Published Role 与有期限 Binding。开发编排器会根据最新 Catalog 重建开发 owner 权限并续期开发绑定；生产环境不得自动续期。

详见《[在线角色与权限治理](../architecture/在线角色与权限治理.md)》和 [ADR-0107](../decisions/ADR-0107-插件权限目录与系统管理授权治理.md)。
