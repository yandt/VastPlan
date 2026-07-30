# Authorization Enforcer

插件 ID：`cn.vastplan.foundation.security.authorization-enforcer`
当前制品版本：`0.3.0`

该 foundation 插件在每个 Backend unit 内以 per-kernel + local-ephemeral + direct 运行，位于用户调用的最终本地 PEP。它严格验证 Catalog、签名 Snapshot、audience 和有效期；未知目录操作弃权给 workload policy，目录内用户操作缺少策略或权限时拒绝。

平台管理、Portal 与 Interaction 的首方无状态 workload 规则以三个独立 Policy Bundle 包编译进同一签名 Enforcer 制品，保留稳定 Checker ID、优先级和单元测试，但不再形成三个额外进程。Bundle 只承载策略内容；注册、调用、上下文裁剪和 fail-closed 强制仍走统一协议总线。

Portal Composer 用户操作从 `0.3.0` 起完全交给签名 Permission Catalog 与在线 Role/Binding；Portal Policy Bundle 对用户调用弃权，不再维护第二份 `portal.*` 角色表。Bundle 仅保留 system break-glass 和 Composer 访问 Kernel Service 的精确 workload 规则；最终资源状态和异人审批仍由 Composer 判定。

默认 `authorization.engine.v1` Native Provider 作为独立插件产出有界 Decision Proof；未来 Cedar、Casbin 或远端 PDP Adapter 可以实现相同协议，但不能绕过 Snapshot 验签和 Enforcer 的最终缓存上限。外部组只从可信 `authorization.directory.v1` 投影读取，再与 Published Group Binding 匹配。
