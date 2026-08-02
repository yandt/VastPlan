# Authorization Enforcer

`cn.vastplan.foundation.security.authorization-enforcer` 是每内核本地授权强制点。它验证 Permission Catalog 与 Ed25519 Policy Snapshot，校验 audience、有效期、Catalog digest、Policy digest 和撤权 revision，并对用户调用 fail-closed。

自 `0.2.0` 起，无状态 workload 策略作为独立 `policybundle/*` 包随同一签名制品运行。`0.3.9` 增加 Approval Provider 稳定协议的调用源策略：允许保留可信 Principal 的插件求值，拒绝用户直连，且不维护消费者或 Provider 插件 ID 白名单；源端和目标端复用 Approval Go SDK 的同一判定。`0.3.10` 为 Marketplace 增加只读 Artifact Catalog 与自身 CredentialRef material lease 的精确白名单，不授予解析、下载或仓库写权限。各 Bundle 保留自己的 contribution ID、优先级和测试，但不再各自启动插件进程。Bootstrap Policy 仍是自举链上的独立运行单元。

授权体系提供两项相互独立的本地能力：

- `permission.checker/foundation.security.authorization-enforcer`：对真实 `CallContext + capability + operation + scope` 就近判定；
- 独立插件 `cn.vastplan.foundation.security.authorization-engine.native`：默认 `authorization.engine.v1` Provider，提供 `prepare/evaluate/explain/health` 和最长五分钟的 Decision Proof。

低/中风险决定最多缓存五分钟，高/关键风险最多缓存五秒；Policy 或 Revocation revision 变化会清空缓存。策略源短暂不可用时只使用未过期 LKG，过期后拒绝。外部组来自可信宿主生成的 `VASTPLAN_AUTHORIZATION_DIRECTORY_GROUPS` 投影，IdP claim 本身从不直接成为权限。
