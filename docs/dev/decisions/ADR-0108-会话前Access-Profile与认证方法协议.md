# ADR-0108 会话前 Access Profile 与认证方法协议

- 状态：已采纳，分阶段实施
- 日期：2026-07-23
- 关联：[ADR-0052](ADR-0052-前端门户内核与多UI设计系统插件.md)、[ADR-0106](ADR-0106-多端统一身份授权与Runner执行租约.md)、[ADR-0107](ADR-0107-插件权限目录与系统管理授权治理.md)

## 背景

Node Portal Kernel 已支持 OIDC 跳转和密封 BFF Session，但没有会话前登录页面。普通 Portal Generation 的选择依赖已认证 Principal/tenant，直接把密码或验证码页面做成普通功能插件会形成循环。若每种登录插件自行提供前端页面，又会绕过 Runtime/Renderer/Shell/Workbench 层级，使公网未认证界面出现不一致 UI、任意脚本和秘密留存风险。

## 决策

1. 新增会话前 Access Profile/Catalog，以请求域名和最长 route 在无 Principal 时选择 Portal/tenant，并精确引用既有 Frontend Platform Profile revision/digest。它不复制四个前端基础层。
2. Shell 插件提供受治理 `access` 模板，Workbench 插件提供统一 `AuthenticationFlow`；登录方式 Provider 只返回固定语义 Step，不提供 HTML、CSS、React 或框架组件。
3. 登录方式实现稳定 `authentication.method.v1`，只开放 `describe/begin/continue/resend/cancel/health`。Provider 无权签发 Cookie、浏览器 Session、角色或 permission。
4. Stateful Authentication Broker 拥有 transaction、限流、方法选择、主体证据校验、Access Profile 绑定、审计和短时 Assertion 签发。Node Portal Kernel 是唯一公网 BFF 与浏览器 Session 签发者。
5. Method Evidence 最长 60 秒；Broker Assertion 最长 30 秒，使用 Ed25519 并绑定 transaction、nonce、subject/issuer、tenant、Portal 和 Node audience。Node 必须验签且一次性消费后才能创建全新 Session。
6. 首期实现密码和临时验证码。密码使用 Argon2id、salt 和托管 pepper；验证码只保存 HMAC/摘要并限制过期、重发与尝试次数。两种替代式登录均为单因素，不能伪装 MFA。
7. Access Profile 只允许内容寻址品牌资产、同源帮助路径、语言和方法 ID；不接受任意 CSS、HTML、外部品牌 URL、Provider 地址或秘密配置。
8. 现有 OIDC 保持生产可用，后续包装为相同协议的 redirect Method；文件会话继续仅用于受控开发。

## 备选方案

- **Node Portal Kernel 硬编码密码/验证码和页面**：实现最快，但每种认证方法都会扩大内核、安全审计面和 UI 分支；拒绝。
- **每种认证插件提供完整前后端登录页**：插件自主性高，但公网未认证脚本、设计一致性、秘密清理和多 Renderer 验收不可控；拒绝。
- **继续只依赖外部 OIDC Provider 页面**：安全成熟，但不能满足本地密码、验证码和 Portal 品牌化方法组合；保留为一种 redirect Method，不作为唯一方案。

## 影响

- Frontend Platform Profile 仍是 Runtime/Renderer/Shell/Workbench 单一真源；新增 Access Profile 只是会话前选择和品牌/方法策略。
- Authentication Broker 与 Method Provider 是插件，Node 只依赖公共契约和窄端口；认证插件的语言和运行隔离可分别选择。
- 登录成功不再从 Method Provider 获取角色。OIDC 当前 roles claim 属于迁移状态，最终应由 Subject Directory/Binding 和 Authorization Policy 投影。
- 多 Node 部署必须共享或可路由认证事务，并对 Assertion 一次性消费执行 CAS；单机内存事务不能作为生产实现。

## 实施记录（2026-07-23）

- 完成 `contracts/schemas/authentication/v1`：Method v1 六操作、固定 Step/Field、Evidence、签名 Assertion、Access Profile/Catalog 及域名最长 route 解析。
- 完成未知字段拒绝、通用错误码、秘密字段 autocomplete/长度、Evidence/Assertion TTL、无角色 Assertion、Access Profile 路由冲突和规范摘要测试。
- Node Portal Kernel 已增加与来源无关的 `AccessCatalogPort`、严格 JSON Schema 校验的本地可信文件适配器、按 Host/最长 route 解析的不可变 Access Generation，以及公共 `GET/HEAD /auth/v1/bootstrap`。公共投影不含 tenant、Portal、Platform Profile 引用或 Logo 摘要。
- Access Profile 补齐会话前最小语言策略；开发编排器在启动 Node 前使用 Go 公共契约校验目录，并核对 Frontend Platform Profile 的精确 revision/digest，热加载网关显式转发 `/auth/*`。
- 本地文件只是一种开发/静态部署适配器，在线控制面后续实现同一窄端口，不改变 HTTP 层。当前尚未实现 `access` Shell 模板、AuthenticationFlow、Broker、密码数据库或验证码发送；现有 OIDC 路径不受本阶段影响。

## 后续实施记录（2026-07-23）

- ADR-0109 已将企业身份 Provider 与 Seed Access 分离并完成 Broker 主链；`/auth/access` 已运行 Portal Foundation 固定的语义 AuthenticationFlow。
- Portal Host 已删除直连 OIDC 会话路径；OIDC 和通用关系数据库密码均作为可选 Provider 插件通过 Broker 工作。OTP/Delivery 仍是后续可选 Provider，不改变本 ADR 的交互协议。
