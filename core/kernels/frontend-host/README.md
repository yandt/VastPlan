# Node Portal Kernel Host

该包是 Portal 的可信 Node 服务宿主，负责 HTTPS、企业 OIDC/BFF 会话、静态宿主、前端交付、Generation 与受监督 SSR Worker。制品验签、授权、集群寻址和插件治理仍通过窄端口调用 Go Backend；浏览器不会取得 OIDC Access/Refresh Token、NATS 凭据或制品仓库凭据。

代码按职责拆分：`config` 只解析启动输入，`identity` 实现身份提供方与密封会话，`assets` 只验证并冻结静态资产，`capabilities` 只适配 Go Backend 窄能力，`runtime` 负责 Activation、不可变快照、内容对象与更新协调，`http` 只处理浏览器协议边界，`workers` 承载服务端 Generation，`server` 只组装生命周期。禁止把这些逻辑重新集中到 `main.ts` 或单一路由文件。

当前已提供安全静态宿主、健康检查、Portal/Interaction/平台管理强类型 BFF，以及认证后的 RuntimeSpec、Recovery、SSE 更新和内容寻址模块交付。交付层读取 Go 可信宿主生成的密封 Browser/Server 双图快照，逐次复核当前 Activation、`PortalSpec` 摘要和实际对象摘要；本机缺失完整 revision 时才从可信 origin 原子冷填充。Server Graph 只进入私有 SSR Worker，不能通过浏览器 API 读取。未实现的 `/v1` 路由仍返回 404，不代理任意 URL。

生产身份使用 OIDC Authorization Code + PKCE。以下示例使用 confidential client；客户端密钥、会话密钥、NKey seed 与 TLS 私钥文件都必须是仅属主可读写的普通文件：

```text
node dist/portal-host.cjs \
  --listen 0.0.0.0:8443 \
  --portal-assets /srv/vastplan/portal \
  --tls-cert /srv/vastplan/tls/portal.crt \
  --tls-key /srv/vastplan/tls/portal.key \
  --identity-provider oidc \
  --oidc-issuer https://id.example.com/ \
  --oidc-client-id vastplan-portal \
  --oidc-client-secret-file /srv/vastplan/private/oidc-client.secret \
  --oidc-client-auth-method client_secret_basic \
  --oidc-redirect-uri https://portal.example.com/auth/callback \
  --oidc-session-key-file /srv/vastplan/private/portal-session.key \
  --oidc-tenant-claim tenant_id \
  --oidc-roles-claim roles \
  --frontend-delivery-cache /srv/vastplan/private/frontend-cache \
  --frontend-delivery-origin /srv/vastplan/private/frontend-origin \
  --nats-servers tls://nats-1.example.com:4222,tls://nats-2.example.com:4222 \
  --addressing-contracts /srv/vastplan/contracts/proto \
  --transport-seed /srv/vastplan/private/portal-host.seed \
  --transport-trust /srv/vastplan/trust/transport-identities.json \
  --nats-tls-ca /srv/vastplan/tls/nats-ca.pem \
  --nats-tls-cert /srv/vastplan/tls/portal-nats.crt \
  --nats-tls-key /srv/vastplan/private/portal-nats.key \
  --composer-logical-service platform.portal-composer \
  --interaction-logical-service platform.interaction-broker
```

OIDC Provider 必须支持 PKCE S256；生产 issuer 与 callback 必须为 HTTPS，callback 必须精确为 `/auth/callback`。宿主验证 ID Token 的 issuer、audience、签名、nonce、state 和有效期，只把 subject、tenant、roles 密封进 AES-256-GCM 的 HttpOnly BFF Cookie。会话最长 1 小时，默认 15 分钟；写请求还必须通过双提交 CSRF。公开客户端省略 secret 并使用 `--oidc-client-auth-method none`。

受控开发环境可使用 `--identity-provider file --session-file ...`，并分别显式声明 `--allow-insecure-http`、`--oidc-allow-insecure` 或 `--allow-insecure-nats`；这些开关不会被生产配置隐式启用。`--frontend-delivery-origin` 不能脱离 `--frontend-delivery-cache` 单独使用。生产 Addressing 必须同时使用 NKey 请求签名、签名信任文档和 NATS mTLS；跨进程 E2E 已验证 TLS 1.3 双向证书、Node 请求签名、Go 响应签名以及 Principal/tenant/roles 权限投影。
