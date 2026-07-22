# Authentication Delivery Webhook

该插件把 `authentication.delivery.v1` 请求发送到企业自有 HTTPS 邮件、短信或消息网关，使 OTP Provider 不依赖具体供应商 SDK。

- 仅接受来自 `enterprise-one-time-code` Provider 调用链的 `deliver` 请求；浏览器和普通业务插件不能直接把它当公开发信接口。
- 每个 Delivery Profile 固定 HTTPS endpoint、允许 channel 和超时。禁止重定向、URL 凭据、明文 HTTP 和超大响应。
- Webhook Bearer 凭证只保存为托管 `CredentialRef`，每次调用通过 audience/tenant 绑定的 Material Lease 临时解封，使用后清零。
- Webhook 必须返回 `{ "accepted": true, "subjectId": "稳定企业主体" }`，或仅返回 `{ "accepted": false }`。该结果只在 OTP Provider 内部使用，浏览器始终看到同形挑战。
- 插件无本地状态，可 active-active 运行；企业可用其他 Node/Java/Go Delivery 插件替换它，而不改变 OTP 或 Portal。

Webhook 请求体包含 `protocol/challengeId/deliveryProfileId/channel/identifier/locale/code/expiresAt`。上游不得记录 `code`，并应自行实现目的地解析、供应商调用、幂等和审计脱敏。
