# Authorization Enforcer

插件 ID：`cn.vastplan.foundation.security.authorization-enforcer`
当前制品版本：`0.1.0`

该 foundation 插件在每个 Backend unit 内以 per-kernel + local-ephemeral + direct 运行，位于用户调用的最终本地 PEP。它严格验证 Catalog、签名 Snapshot、audience 和有效期；未知目录操作弃权给 workload policy，目录内用户操作缺少策略或权限时拒绝。

默认 `authorization.engine.v1` Native Provider 作为独立插件产出有界 Decision Proof；未来 Cedar、Casbin 或远端 PDP Adapter 可以实现相同协议，但不能绕过 Snapshot 验签和 Enforcer 的最终缓存上限。外部组只从可信 `authorization.directory.v1` 投影读取，再与 Published Group Binding 匹配。
