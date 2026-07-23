# @vastplan/shared-state

Node Worker 插件使用的 `state.shared.v1` 客户端。请求只包含局部 scope kind、namespace、key、value 和 CAS revision；tenant、插件与 Runtime scope 由可信宿主推导。

客户端不直接连接 NATS/数据库，不持有基础设施凭证。`service` scope 按已验签 RuntimeScope 隔离，`tenant` scope 额外绑定可信 `CallContext.tenant_id`。
