# ADR-0109 种子访问与企业身份 Provider 分离

- 状态：已采纳，分阶段实施
- 日期：2026-07-23
- 关联：[ADR-0047](ADR-0047-多语言运行驱动与第三方隔离边界.md)、[ADR-0090](ADR-0090-插件配置与托管凭证闭环.md)、[ADR-0106](ADR-0106-多端统一身份授权与Runner执行租约.md)、[ADR-0107](ADR-0107-插件权限目录与系统管理授权治理.md)、[ADR-0108](ADR-0108-会话前Access-Profile与认证方法协议.md)

## 背景

平台管理中心在首次安装时可能没有数据库、企业用户目录或可用的 OAuth/OIDC 服务，但完成初始化后又必须接入企业现有身份体系。若把用户表、密码、OIDC 或 LDAP 固化到内核，VastPlan 会从微内核变成另一套用户系统；若把首次管理员也作为普通企业用户，则数据库或企业 IdP 故障时无法恢复平台。

## 决策

1. VastPlan 不内置普通用户系统。内核只维护可信 Session/Assertion 校验、Credential Material Lease、进程隔离和调用身份边界，不保存企业用户、密码、组织、Group 或角色。
2. 普通登录全部由 `authentication.provider` 插件贡献。一个部署可同时安装 OIDC/OAuth、SAML、LDAP、数据库用户、Passkey 或其他 Provider，并通过不可变 Authentication Provider Profile/Catalog 选择具体实例。
3. Access Profile 只声明门户允许的 Method ID；服务端 Catalog 把 tenant/Portal/method 唯一解析到已发布 Provider Profile。浏览器不能提交 Provider 地址或越过 Catalog 选择未授权实现。
4. Provider Profile 只保存贡献 ID、配置文档引用、用途、Method、稳定 subject namespace 和能力依赖，不保存用户、密码、Token、Client Secret、角色或 Group。秘密仍由插件配置和 Credential Material Lease 管理。
5. Provider 的稳定主体键由 `providerProfileId + issuer + subject` 构成。外部 claim、Group、组织和角色只能作为身份材料交给独立 Directory/Authorization 流程，不得直接变成 permission。
6. Provider 使用 `Draft → Validated → Tested → Approved → Published → Retired` 的管理生命周期；运行就绪另用 `Unknown/Blocked/Ready/Degraded/Failed` 表示。已发布 Provider 可因数据库或网络依赖暂时 Blocked，而不篡改批准记录。
7. Provider 声明的能力依赖必须同时出现在签名 Manifest `runtime.requires`。OIDC/SAML 等不依赖业务数据库的实现可以先就绪；数据库用户 Provider 在 `database.provider` 与 Schema 未就绪时保持 Blocked，不能拖垮 Seed Access 或其他 Provider。
8. 首次安装与灾难恢复使用独立 Seed Access Plane。它只拥有最小 Seed Operator、一次性引导、Provider 配置、连通/认证测试、交接和恢复能力，采用数据库无关的受保护文件/系统密钥 Store，不演化为企业用户目录。
9. Seed 权限必须在外部 Provider 完成一次真实登录、主体稳定映射、内部授权绑定、正常 Session 签发和恢复通道配置后原子交接。交接完成即撤销临时管理员；恢复重新开启需本机运维权限、短租约和审计。
10. 第一方 Broker/Catalog/Seed Authority/File Store 可由一个受信 Go Runtime 进程承载；网络协议型 OIDC/OAuth Provider 优先 Node；数据库 Provider 依据驱动生态选择语言。第三方 Provider 默认独立隔离，语言和运行形态分别决策。

## 备选方案

- **内核自带一套用户/密码表，再外接 OAuth**：首次使用直观，但内核永久承担账号生命周期、密码学和企业目录兼容；拒绝。
- **只有独立 Seed 账号，不接企业用户系统**：恢复简单，但正常运营无法复用企业离职、MFA、条件访问和审计能力；拒绝。
- **每个认证插件直接签发 Session 和角色**：插件自治，但会形成多套信任根、撤权和权限解释；拒绝。
- **所有 Provider 必须依赖同一平台数据库**：实现统一，但数据库未配置或故障会同时切断初始化、OIDC 和恢复；拒绝。

## 影响

- `authentication.method.v1` 继续作为交互协议；新增 Provider Profile/Catalog 和 Manifest `authenticationProviders` 贡献，不重写已有 Access Profile。
- Authentication Broker、Provider Catalog 和 Seed Authority 属于 Foundation 能力，不是内核用户系统；它们仍可作为插件独立升级和替换实现。
- 在线角色插件只汇总签名权限目录并绑定稳定主体，不创建或校验企业用户。
- Portal、Mobile 和 Runner 可使用不同认证载体，但必须汇聚到同一稳定主体和 Authorization Policy；外部 Token 验证将以 Provider 能力接入，不能让各内核自行解析 claim 授权。

## 实施记录（2026-07-23）

- 完成 `AuthenticationProviderProfile`、`AuthenticationProviderCatalog`、Binding 唯一路由、生命周期与运行就绪公共契约及 JSON Schema。
- 完成稳定主体键、规范摘要、数据库依赖 Blocked、模糊 Method 路由和秘密/用户字段拒绝测试。
- Plugin Manifest 新增 `authenticationProviders`，运行态扩展点新增 `authentication.provider`；Provider 依赖必须闭合到 `runtime.requires`。
- Seed Authority、Provider Broker、管理 Workbench 与首个 OIDC Provider 继续按本文阶段实施。
- 完成 Authentication Broker：按 tenant/Portal/method 唯一路由、服务端注入 Profile ID、transaction 锁定 Provider、Provider 输出复验与 TTL 上限；静态文件 Catalog 只是同一窄接口的 Seed 适配器。
- 完成首个 Node OIDC Provider 的 public-client 路径：Authorization Code + PKCE、state/nonce、JWKS RS256/ES256、issuer/audience/azp/time 校验和一次性回调；confidential client 必须等待 Material Lease，禁止把 client secret 放入普通配置。
- Provider 管理面完成 CAS 生命周期、职责分离审批、Broker 签名真实认证测试，以及 Provider Catalog 与 Access Catalog 的同代原子发布；管理 UI 只使用统一 Workbench 契约。
- Broker 完成短时 Ed25519 Assertion 签发，Assertion 绑定 Provider Profile、transaction、tenant、Portal、audience 与稳定 subject；Provider Evidence 不再能直接建立平台 Session。
- Node OIDC Provider 完成 confidential client 路径：配置只保存 `clientSecretRef`，秘密通过 audience/tenant/purpose 绑定的单次 Material Lease 取得并在回调后清零。
- 新增通用关系数据库用户 Provider：通过 `foundation.data.relational.runtime` 与现有连接池查询，支持 `?`/`$1` 参数方言，使用有界 Argon2id PHC 验证、伪校验路径与统一凭据错误；它不属于内核，也不构成平台用户系统。
- Portal Host 删除直连 OIDC 模式；生产身份只接受 Authentication Broker。Node 本地验签后必须由 leader-routed Broker 原子消费 Assertion，再通过独立 Authorization Session 插件读取受签名 Policy Snapshot，外部 claim/Group/Role 不得直接进入权限上下文。
- 新增 `cn.vastplan.foundation.security.authorization-session` Go 插件与稳定主体哈希；会话只投影无资源/属性约束的 allow 权限，deny 与撤销优先，细粒度约束仍交给 Authorization Engine。
- Seed Handoff 接入 Broker Assertion 和签名 Policy Snapshot 双证明；可信 BFF 在 HttpOnly 密封 Session 内转交证明，企业主体完成授权绑定后经 CAS 进入 `EnterpriseActive`，Seed 登录随即关闭。
- `/auth/access` 完成会话前语义 AuthenticationFlow；页面只渲染受限步骤，Provider 无法注入前端代码、样式或任意 Schema。
- 未发布但已 `Validated` 的 Provider Profile 可由授权管理员启动隔离真实认证测试。Broker 锁定测试用 Profile 与 audience，Node 原子消费 Assertion 后只写入短时密封测试证明，不创建、替换或提权正常 Session；管理 BFF 只接受该服务端证明，拒绝浏览器自报 Assertion。
