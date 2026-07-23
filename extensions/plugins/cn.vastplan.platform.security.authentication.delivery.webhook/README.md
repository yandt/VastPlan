# Authentication Delivery Webhook

该插件把 `authentication.delivery.v1` 请求发送到企业自有 HTTPS 邮件、短信或消息网关，使 OTP Provider 不依赖具体供应商 SDK。

- 仅接受来自 `enterprise-one-time-code` Provider 调用链的 `deliver` 请求；浏览器和普通业务插件不能直接把它当公开发信接口。
- 每个 Delivery Profile 是独立 `cfgp_*` 资源，固定 HTTPS endpoint、允许 channel 和超时。禁止重定向、URL 凭据、明文 HTTP 和超大响应。
- Webhook Bearer 凭证只保存为托管 `CredentialRef`，每次调用通过 audience/tenant 绑定的 Material Lease 临时解封，使用后清零。
- Webhook 必须返回 `{ "accepted": true, "subjectId": "稳定企业主体" }`，或仅返回 `{ "accepted": false }`。该结果只在 OTP Provider 内部使用，浏览器始终看到同形挑战。
- 插件同时实现 `configuration.resource.v1`，以租户隔离、Active CAS 和 prepare/commit/abort/status 管理 Profile；状态使用部署指定的私有 `stateFile`、`0600` 原子替换与有界文件。
- 当前 File State Provider 只能按 `leader + leader-owned` 运行，不能伪装为 active-active。未来若切换外部共享一致存储 Provider，才能在协议不变的前提下开放 queue 多副本。
- Node 实现继续进入该服务的共享 `node-worker`，不会为每个 Profile 新增进程；企业可用其他 Node/Java/Go Delivery 插件替换它，而不改变 OTP 或 Portal。

Webhook 请求体包含 `protocol/challengeId/deliveryProfileId/channel/identifier/locale/code/expiresAt`。上游不得记录 `code`，并应自行实现目的地解析、供应商调用、幂等和审计脱敏。

Profile 只能经插件配置协调器的独立资源 Saga 创建、更新和删除。创建必须提交 Authorization 材料；更新留空表示保留当前引用，新值会形成 replacement；旧引用由本插件在提交后以 owner 身份退休。查询与 Workbench 永不返回 CredentialRef handle 或 material。
