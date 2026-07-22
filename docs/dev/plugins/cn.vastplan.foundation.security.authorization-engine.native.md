# Native Authorization Engine

插件 ID：`cn.vastplan.foundation.security.authorization-engine.native`
当前制品版本：`0.1.0`

该 foundation Provider 插件实现默认 Go `authorization.engine.v1`，提供 prepare、evaluate、explain、health 和最长五分钟的 Decision Proof。它只接受可信 system caller，并与每内核 Enforcer 分开部署：Enforcer 因此保持纯 permission checker，可合法重复附着多个 platform service unit；Engine Provider 继承 `platform.authorization` 的 `leader / leader-owned / cluster / security` 运行策略。

Provider 不能代替 Enforcer 的 Snapshot 验签、audience、LKG、撤权和缓存上限，也不能创建 Role 或修改 Permission Catalog。
