# Node Portal Kernel Host

该包是 Portal 的可信 Node 服务宿主，逐步接管原 Go Portal Edge 的 HTTP、会话、BFF、静态宿主、前端交付、Generation 与受监督 Worker。制品验签、授权、集群寻址和插件治理仍通过窄端口调用 Go Backend。

代码按职责拆分：`config` 只解析启动输入，`assets` 只验证并冻结静态资产，`http` 只处理协议边界，后续 `backend`、`delivery`、`generation` 与 `workers` 分别实现可信端口与运行工作流。禁止把这些逻辑重新集中到 `main.ts`。

当前阶段只提供安全静态宿主、健康检查和 fail-closed 的 `/v1` 边界；未实现的 BFF 路由返回 404，不代理任意 URL。
