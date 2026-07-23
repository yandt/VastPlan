# @vastplan/configuration-scoped

Node.js 插件消费 `configuration.scoped.v1` 的客户端。`resolve` 与 `watchRevision` 只复用宿主提供的可信 `CallContext`，请求不接受 tenant、subject、插件 ID 或配置 ID。

客户端严格拒绝未知响应字段、复算非敏感 values 摘要，并区分 revision 0 的签名 Seed 与 revision > 0 的 Active 配置。它是插件 Worker 内的普通库，不创建独立进程。
