# ADR-0106 多端统一身份授权与 Runner 执行租约

- 状态：已采纳，实施中
- 日期：2026-07-21
- 关联：[ADR-0021](ADR-0021-权限判定强制点.md)、[ADR-0027](ADR-0027-NATS生产安全与最小权限.md)、[ADR-0054](ADR-0054-跨端体验契约与交互协调.md)、[ADR-0061](ADR-0061-统一调用信封与受众投影.md)

## 背景

Web、Mobile、Runner 和服务进程不能安全共用一种凭证载体。浏览器适合 HttpOnly BFF Session，Mobile 适合短期 OAuth Token，Runner 还必须区分登录用户、受管设备和本地执行工作负载。若各端维护自己的权限含义，角色、撤权、跨租户规则和插件权限目录会漂移。

## 决策

1. 各端使用适合自身的认证载体，但可信入口统一投影为一个 Wire `CallContext`。上下文包含 Subject、立即 Caller、可选 Device、Delegation、Scope、Authorization Proof、Trace 和 CredentialRef；插件不得从 payload 自报这些字段。
2. Web 使用 OIDC Authorization Code + PKCE 和服务端 BFF Session；浏览器只持有 HttpOnly/Secure/SameSite Cookie 与 CSRF。Mobile 使用短期 Access Token、Refresh Rotation 和设备绑定。Runner 使用设备身份以及用户或工作负载身份，绝不复用 Portal Cookie。
3. 内核 PEP 继续强制所有业务调用。工作负载 capability、主体 permission、tenant/project、设备策略、插件清单、对象状态和可选 Execution Lease 必须同时满足。
4. `cn.vastplan.foundation.security.authorization-enforcer` 提供就近判定；`cn.vastplan.platform.security.authorization-policy` 是角色、策略 revision、撤权、签名快照和租约真相源；`cn.vastplan.platform.configuration.role-management` 提供在线管理，不参与最终强制。
5. 权限代码由签名插件 Manifest 声明并受命名空间保护。安装插件不自动授权；UI 隐藏只用于体验，Backend 必须重新判定。`is_admin` 不再形成隐式全局绕过，break-glass 必须短期、显式并强审计。
6. Runner 离线/长任务使用签名 `RunnerExecutionLease`，绑定 tenant/project、subject、runner/device key、App Profile digest、插件 digest、workflow、资源选择器、policy revision、有效期和次数。高风险权限不得离线。
7. 原始 Cookie、Access/Refresh Token、Runner 长期凭证和 Credential Material 不进入普通插件、SSR Worker、Broker 状态或审计日志。
8. Policy 不可用时，高风险请求拒绝；低风险仅可在未过期签名快照/决定 TTL 内继续。策略过期、签名错误或撤权 revision 超前均 fail-closed。

## 备选方案

- **所有端共用携带完整权限的 JWT**：浏览器暴露、权限膨胀且撤权滞后；拒绝。
- **每次请求只调用中央 PDP**：一致但把全平台可用性和延迟绑定到单服务；拒绝，采用签名快照加高风险在线复核。
- **Runner 持有长期万能 Token**：设备被复制或离线失陷后影响无界；拒绝。

## 影响

- CallContext 保持唯一 Wire 契约，但以窄子视图限制消费者，禁止继续向 metadata 塞业务身份字段。
- Portal、Mobile、Runner 和 Backend 必须共享权限代码、Decision 契约和安全测试语料。
- Runner Profile 领取只是设备资格检查，不能替代每次任务 Execution Lease。

## 实施进展（2026-07-22）

- Node Portal Kernel 已实现生产 OIDC Authorization Code + PKCE BFF：discovery/JWKS、state、nonce、S256、issuer/audience/ID Token 有效期验证以及可配置 tenant/roles claim 映射均 fail-closed。
- OIDC Access/Refresh Token 不写浏览器 Cookie、不进入 SSR Worker 或 Backend 调用。浏览器会话只包含经验证的 Principal 投影，并使用宿主私钥材料通过 AES-256-GCM 密封；登录事务与会话都具有独立短期有效期。
- Portal Host 将会话中的 subject、tenant 与 roles 固定投影到 Addressing 调用；E2E 已覆盖 OIDC 登录到 BFF capability 投影，以及 Node↔Go TLS 1.3 mTLS、NKey 请求/响应签名和签名目录验证。
- Mobile Token、Runner 设备身份与 `RunnerExecutionLease` 仍按本 ADR 的独立载体推进，不能复用 Portal Session。
