# Authorization Policy

插件 ID：`cn.vastplan.platform.security.authorization-policy`
当前制品版本：`0.6.0`

该平台全栈插件的 Backend 是在线授权唯一写入真相源，以 `leader + external-shared + cluster` 运行。它消费签名制品构建的 Permission Catalog，管理 Role revision、Subject Binding revision、不同主体审批、即时撤权、审计和 Ed25519 Policy Snapshot。运行期账本写入受内核治理的 service-scope Shared State，使用 leader fence、Store CAS 与独立业务 generation；插件不直接持有 NATS 凭据。可选本地 bootstrap 文件只允许在权威 key 不存在时导入一次，Shared State 故障绝不回退。

Frontend 提供权限目录、Role、Subject Binding 和审计四个 Workbench Collection 页面，只通过 Portal Management Binding 调用 `platform.authorization`，不保存授权状态、不签发快照，也不直接依赖具体 UI 框架。自 `0.2.0` 起，原 `cn.vastplan.platform.configuration.role-management` 已按 ADR-0152 并入本插件，使管理 UI 与对应 Backend 协议共用插件身份、版本和发布事务。

`0.2.2` 只迁移授权治理账本的真相源；签名私钥仍由可信启动环境提供，签名 Snapshot 当前仍物化为本地只读文件供同节点 Enforcer 消费。跨节点 Snapshot 发布与 LKG 分发必须另走受签名保护的发布协议，不能通过放宽 Shared State 作用域实现。

Authorization Policy 使用 `leader / external-shared` 服务单元；默认 Native Engine 使用独立的 `leader / leader-owned` 服务单元。两者通过集群能力目录调用，不能为了共进程部署而篡改任一插件的签名运行策略。

首次安装的 `platform.owner` 不是 `is_admin/platform.admin` 旁路，而是由 Seed Authority 物化的 Published Role 与有期限 Binding。开发编排器会根据最新 Catalog 重建开发 owner 权限并续期开发绑定；生产环境不得自动续期。

自 `0.6.0` 起，Role 写入协议接受 `exact` 与受限 `glob` 权限选择器。`*` 匹配一个点分段，`**` 匹配一个或多个点分段，且首段必须是字面命名空间；正则表达式、部分分段通配和前导通配均被拒绝。选择器在 Role revision 创建或更新时绑定当前 Catalog 编译为精确权限集，运行期 IR、Enforcer、Session 和多端客户端不承担模式匹配。显式开发 bootstrap 还会选择一次性 `seed-owned` 协调策略，以受信 Seed Authority 身份补齐后设置的 Seed Owner Role/Binding，并把同一基线同步进 Shared State；普通启动注入禁用策略，不创建授权状态。

详见《[在线角色与权限治理](../architecture/在线角色与权限治理.md)》和 [ADR-0107](../decisions/ADR-0107-插件权限目录与系统管理授权治理.md)。
