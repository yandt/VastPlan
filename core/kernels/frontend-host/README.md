# Node Portal Kernel Host

该包是 Portal 的可信 Node 服务宿主，负责 HTTPS、Broker Assertion/BFF 会话、静态宿主、前端交付、Generation 与受监督 SSR Worker。OIDC、SAML、LDAP、数据库用户等协议由可选择的认证 Provider 插件实现；制品验签、授权、集群寻址和插件治理仍通过窄端口调用 Go Backend。浏览器不会取得上游 Access/Refresh Token、NATS 凭据或制品仓库凭据。

代码按职责拆分：`config` 只解析启动输入，`identity` 实现身份提供方与密封会话，`assets` 只验证并冻结静态资产，`capabilities` 只适配 Go Backend 窄能力，`runtime` 负责 Activation、不可变快照、内容对象与更新协调，`http` 只处理浏览器协议边界，`workers` 承载服务端 Generation，`server` 只组装生命周期。禁止把这些逻辑重新集中到 `main.ts` 或单一路由文件。

当前已提供安全静态宿主、健康检查、Portal/Interaction/平台管理强类型 BFF，以及认证后的 RuntimeSpec、Recovery、SSE 更新和内容寻址模块交付。交付层读取 Go 可信宿主生成的密封 Browser/Server 双图快照，逐次复核当前 Activation、`PortalSpec` 摘要和实际对象摘要；本机缺失完整 revision 时才从可信 origin 原子冷填充。Server Graph 只进入私有 SSR Worker，不能通过浏览器 API 读取。未实现的 `/v1` 路由仍返回 404，不代理任意 URL。

生产身份使用 Authentication Broker；具体企业协议不再配置到 Portal Host。Assertion 信任文件由 Broker 公钥生成，会话密钥、NKey seed 与 TLS 私钥文件必须是仅属主可读写的普通文件：

```text
node dist/portal-host.cjs \
  --listen 0.0.0.0:8443 \
  --portal-assets /srv/vastplan/portal \
  --tls-cert /srv/vastplan/tls/portal.crt \
  --tls-key /srv/vastplan/tls/portal.key \
  --identity-provider broker \
  --access-profile-catalog /srv/vastplan/config/authentication-providers.json \
  --authentication-assertion-trust-file /srv/vastplan/trust/authentication-assertions.json \
  --portal-session-key-file /srv/vastplan/private/portal-session.key \
  --authentication-broker-logical-service identity-broker \
  --authorization-session-logical-service authorization-session \
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

Portal 只接受最长 30 秒、绑定 transaction/tenant/Portal/audience/Provider Profile 的 Ed25519 Broker Assertion；本地验签后仍必须回到 leader-routed Broker 原子消费，再由独立 Authorization Session 插件把稳定主体映射成内部权限。外部 Provider 的 Group/Role claim 不会直接进入 Session。会话使用 AES-256-GCM HttpOnly Cookie，最长 1 小时、默认 15 分钟；会话前写请求同时校验 Origin、Fetch Metadata 和双提交 CSRF。

受控开发环境可使用 `--identity-provider file --session-file ...`，并分别显式声明 `--allow-insecure-http` 或 `--allow-insecure-nats`；这些开关不会被生产配置隐式启用。`--frontend-delivery-origin` 不能脱离 `--frontend-delivery-cache` 单独使用。生产 Addressing 必须同时使用 NKey 请求签名、签名信任文档和 NATS mTLS。
