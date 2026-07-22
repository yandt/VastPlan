# VastPlan Enterprise OIDC Provider

首个企业身份 Provider，使用 Node.js 实现 Authorization Code + PKCE、state、nonce、JWKS 签名校验和严格 issuer/audience/azp/time 绑定。它只返回短时 Authentication Evidence，不签发 VastPlan Session、角色或权限。

选择 Node.js 是因为 OIDC/JWT/供应商兼容生态和热升级能力更适合网络协议 Provider；该选择不改变统一 `authentication.method.v1`。第一方插件默认运行在共享 Node Worker Runtime。当前短时 PKCE transaction 使用 leader-owned 路由保证回调回到同一实例；引入共享 Transaction Store 后才可改为 active-active。第三方同类实现仍按发布者策略隔离。

同时支持 PKCE public client 与 confidential client。confidential Profile 只保存 owner/purpose/version 绑定的 `clientSecretRef`，Node Runtime 为每次 token exchange 生成一次性 X25519 接收密钥，通过 `kernel.credential.material-lease` 解封最长 30 秒的 AES-GCM 包，并在回调结束时清零 material Buffer；禁止把 `clientSecret` 写入非敏感配置或环境 JSON。

插件配置支持多个不可变 Provider Profile；Broker 注入服务端选定的 `providerProfileId`。所有 endpoint 和 redirect URI 必须为无凭据 HTTPS URL，回调只接受 `code/state` 或 `error/state`，Token 永不返回 Portal。

架构见《[企业身份与种子访问](../../../docs/dev/architecture/企业身份与种子访问.md)》。
