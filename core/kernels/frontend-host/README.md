# Node Portal Kernel Host

该包是 Portal 的可信 Node 服务宿主，逐步接管原 Go Portal Edge 的 HTTP、会话、BFF、静态宿主、前端交付、Generation 与受监督 Worker。制品验签、授权、集群寻址和插件治理仍通过窄端口调用 Go Backend。

代码按职责拆分：`config` 只解析启动输入，`assets` 只验证并冻结静态资产，`capabilities` 只适配 Go Backend 窄能力，`runtime` 负责 Activation、不可变快照、内容对象与更新协调，`http` 只处理浏览器协议边界，`server` 只组装生命周期。后续 SSR/服务端 Generation 进入独立 `workers`，禁止把这些逻辑重新集中到 `main.ts`。

当前已提供安全静态宿主、健康检查、Portal/Interaction/平台管理强类型 BFF，以及认证后的 RuntimeSpec、Recovery、SSE 更新和内容寻址模块交付。交付层读取 Go 物化器生成的兼容快照，逐次复核当前 Activation、`PortalSpec` 摘要和实际对象摘要；本机缺失完整 revision 时才从可信 origin 原子冷填充。未实现的 `/v1` 路由仍返回 404，不代理任意 URL。

构建后可用以下核心参数启动：

```text
node dist/portal-host.mjs \
  --listen 127.0.0.1:8443 \
  --portal-assets /srv/vastplan/portal \
  --session-file /srv/vastplan/private/sessions.json \
  --tls-cert /srv/vastplan/tls/portal.crt \
  --tls-key /srv/vastplan/tls/portal.key \
  --frontend-delivery-cache /srv/vastplan/private/frontend-cache \
  --frontend-delivery-origin /srv/vastplan/private/frontend-origin \
  ...Addressing mTLS/NKey 参数
```

`--frontend-delivery-origin` 不能脱离 `--frontend-delivery-cache` 单独使用。生产仍必须配置 Addressing mTLS/NKey；本地明文 HTTP/NATS 只有显式开发开关才允许。Catalog 验签/物化能力、服务端 Generation/SSR Worker 和生产切流尚未迁移完成，因此 Go Edge 在当前阶段仍保留为生产后备。
