# ADR-0103 Node Portal Kernel 渐进替代 Go Portal Edge

- 状态：已采纳，实施中
- 日期：2026-07-21
- 关联：[ADR-0052](ADR-0052-前端门户内核与多UI设计系统插件.md)、[ADR-0062](ADR-0062-Frontend可信ESM制品与运行描述.md)、[ADR-0076](ADR-0076-Portal-Edge分布式快照交付.md)、[ADR-0078](ADR-0078-Frontend事务式热替换与插件生命周期.md)

## 背景

现有 Portal 由 Go Portal Edge 提供认证 BFF、静态宿主、可信制品交付和 Activation，由浏览器 React Kernel 完成装配。继续增加 SSR、服务端前端模块、细粒度 ESM 图和 React Engine 生命周期时，若另建 Node Frontend Runtime，会形成 Go Edge、Node Runtime、浏览器 Kernel 三套状态机和两次前端契约解释。

Node.js 对 HTTP/BFF 并无能力缺口，且与 ESM、React SSR、构建器、Source Map、Worker 和 HMR 共用生态。真正需要保留的不是 Go 语言边界，而是可信入口、插件执行和 Backend 权限之间的安全边界。

## 决策

1. Frontend 内核的生产宿主统一为 TypeScript/Node.js 实现的 `Node Portal Kernel`。它最终替代 `backend portal-edge`，但不替代 Go Backend Kernel。
2. Node 主进程负责 HTTPS、OIDC/BFF、Cookie、CSRF、CSP、RuntimeSpec、内容寻址交付、Activation 和 Generation 协调。SSR、插件 `serverEntry`、解包和重 CPU 工作必须进入受监督 Worker，不得阻塞主事件循环。
3. Go Backend 继续拥有制品签名与发布者信任、Catalog 生命周期、权限强制、策略、凭证、Composer、Interaction Broker 和集群控制。Node 通过语言无关 Protocol/Addressing 契约调用这些能力，不复制第二套私有 REST 控制面。
4. Node 工作负载使用独立传输身份和精确 capability allowlist。只有被信任文档授予 delegation 的 Portal Host 才可投影已验证用户；Backend 仍重建 Caller 并在 `Host.Invoke` 重新授权。
5. 默认每个 Portal 服务一个 Node 主进程和有界 Worker 池。首方可信服务端前端模块可使用 Worker；第三方代码仍必须按发布者与部署策略进入独立进程、sandbox、container 或 WASM。
6. 迁移采用渐进替换：先冻结并记录 Go Edge 行为，Node 并行实现契约对照测试；测试环境切换并完成真实 OIDC/TLS/签名制品验收后，才删除 Go Edge。迁移期 Go Edge 只接受安全修复。
7. Go Edge 删除是完成条件。不得以长期双入口作为最终状态，避免会话、Header、权限和 Activation 行为漂移。

## 备选方案

- **保留 Go Edge，新增独立 Node Runtime**：复用最多，但长期拥有两套前端服务状态机和跨进程 Generation 提交；作为迁移回退而非最终结构。
- **全部逻辑运行在一个 Node 主线程**：开发最直接，但插件死循环、OOM 或 SSR 阻塞会带走认证和健康入口；拒绝。
- **整个 Backend 改为 Node.js**：与本次前端内核边界无关，会重写已经封板的集群、供应链和执行驱动；拒绝。

## 影响

- 正面：Browser/Server 模块图、SSR、React Engine、HMR 和诊断使用一套 TypeScript 工具链。
- 正面：减少 Go/Node 之间的前端专有 DTO 和两阶段装配。
- 代价：必须用契约对照测试覆盖现有约 6,200 行 Go Edge 行为，迁移不能靠页面能打开作为完成证据。
- 安全：Node 主进程进入前端可信计算基；生产依赖必须锁定、可复现、签名且禁止运行时 npm install。

## 实施进展（2026-07-21）

- Node Portal Kernel 基础宿主已完成 HTTPS、文件会话、CSRF、CSP、静态资产边界和未知 `/v1` fail-closed。
- 新增 `@vastplan/addressing-node`：直接复用 Addressing v1 的 NATS + Protobuf Wire、签名 Capability Directory 与 NKey 传输身份，不建立私有 REST 代理。目录记录执行形状、服务策略、租约、签名和调用方 allowlist 校验；响应公钥必须绑定本次所选的公告实例。
- Node/Go 使用共享确定性 NKey golden 验证签名字节兼容；Go Router 同时修正远端公告签名身份应绑定公告 `node_id`、而不是绑定接收方 node 的错误。
- 跨进程 E2E 已覆盖 Node 读取 Go 签名目录、Node 请求签名、Protobuf v1 调用、Go 响应签名和 Node 响应实例身份绑定，不再只以两端各自单元测试作为互通证据。
- Portal Host 已接入可选 Addressing 生命周期和严格启动配置。Application Draft、Platform Profile、Management Binding、Activation、Frontend Test Target 与 Test Release 的 HTTP 工作流已通过窄 Composer 端口迁移；各资源拥有独立路由模块。写操作先验证服务端会话和双提交 CSRF，再执行 1 MiB 有界 JSON 解码，revision/source/resource ID 只能由受信 URL 路径投影。未知能力和路由继续 fail-closed。
- Interaction Web 端点已通过独立 `InteractionPort` 迁移，浏览器只能调用 list/get/present/respond；可信宿主固定投影 `frontend` surface 和已验证 Principal。Composer 与 Interaction 共享 Addressing/CallContext 基础适配，但各自持有独立 operation allowlist。
- 后续仍需迁移平台管理、RuntimeSpec/内容对象端点，并完成真实 NATS+mTLS+权限对照 E2E 后切换流量。
