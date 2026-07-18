# ADR-0034 Backend 协议资源边界

- 状态：已采纳
- 日期：2026-07-16
- 关联：[ADR-0027 NATS 生产安全与最小权限](ADR-0027-NATS生产安全与最小权限.md)、[ADR-0029 跨服务双向流与持久事件](ADR-0029-跨服务双向流与持久事件.md)、[ADR-0031 Backend 内核 1.0 封板与工程门禁](ADR-0031-Backend内核1.0封板与工程门禁.md)

## 背景

协议正确不等于生产可用。插件进程、内核宿主和跨节点 addressing 如果允许无界 payload、metadata、并发 goroutine 或 pending 请求，单个错误插件或突发流量就能耗尽宿主内存和文件描述符。只有某一段限制也不够：SDK 接受、Host 拒绝，或 NATS 接受、gRPC 拒绝，都会形成不可预测的故障边界。

默认 deadline 与 drain 同样属于资源边界。调用方忘记设置 deadline 时，处理器不能无限占用槽位；升级排空也不能永久阻塞控制器。

## 决策

### 1. 单一限额契约

`core/shared/go/protocollimit.Limits` 是 Host、第一方 SDK 和 addressing 的单一真源。零值不能关闭保护，而是收敛到以下默认值：

| 资源 | 默认值 | 边界语义 |
|---|---:|---|
| 一元 payload / 最终 payload | 4 MiB | 更大对象改用对象存储引用或分片流 |
| 单个流帧 | 1 MiB | 让 HTTP/2 背压及时生效 |
| CallContext / 传输 metadata | 16 KiB | 防止标签、凭证引用和 header 膨胀 |
| 并发调用 | 256 | 在创建 handler goroutine 前占槽 |
| pending 请求 | 512 | request-reply 关联表与 NATS subscription 队列均有界 |
| 默认 deadline | 30 秒 | 取调用方、CallContext 与默认值中的最早截止时间 |
| drain timeout | 30 秒 | 排空超时后由上层进入失败处置，不无限等待 |

部署可以显式提高字段，但必须配套容量测试。`MaxMessageBytes` 由最大业务 body 加固定 protobuf 信封余量推导，不能独立漂移。

### 2. 每一跳都执行边界

- Host gRPC 服务与 SDK 客户端同时限制收发消息和 header；Host/SDK 分别验证输入、输出 payload 与 `CallContext`。
- Host 会话 pending 表、SDK HostCall pending 表和回调 dispatch 都在创建等待者或 goroutine 前 fail-fast。
- addressing 本地 fast path、NATS request-reply、Core NATS 事件和 gRPC 双向流执行同一份边界；NATS subscription 通过消息数和字节数双上限防止客户端内部无界排队。
- gRPC 流同时限制并发 stream、起始 payload、每帧和最终 payload。HTTP/2 背压只负责速率，不替代单帧硬上限。

资源拒绝使用稳定错误码 `resource.payload_too_large`、`resource.metadata_too_large`、`resource.concurrency_limited` 和 `resource.queue_full`。能形成可信 `CallResult` 时返回应用层错误；信封尚未建立或跨服务传输失败时使用带同一 code 的 transport error。

### 3. deadline 与 drain 传播

入口把 `context.Context`、`CallContext.deadline_unix_ms` 和统一默认值取最早者，创建派生 context，并把最终 deadline 写入克隆后的 `CallContext`。调用方原对象不被修改，下游插件和远端服务因此看到同一截止时间。

Host drain 在调用方未提供截止时间时自动使用 `DrainTimeout`；DRAIN/SHUTDOWN 生命周期 Ack 也使用 drain 时限。超时是失败结果，不允许静默视作排空成功。

## 备选方案

- **只依赖 gRPC/NATS 默认限制**：默认值不覆盖 handler goroutine、业务 payload、pending 关联表和本地 fast path。拒绝。
- **各模块自行选择默认值**：短期改动小，但多跳调用会在不同位置随机失败且难以容量规划。拒绝。
- **只用信号量等待，不拒绝**：把无界 goroutine 换成无界等待队列，仍可耗尽内存并放大尾延迟。拒绝。
- **不设默认 deadline，强制调用方传入**：遗漏时生产故障不可收敛，不能作为内核安全默认。拒绝。

## 影响

- 正面：恶意或错误插件、流量尖峰和遗漏 deadline 都被限制在可配置的固定预算内。
- 代价：历史上依赖大一元 payload 的调用需要改用流或外部对象引用；提高上限必须显式配置。
- 风险：限额值不是性能承诺，仍需 benchmark、故障注入和 soak 验证具体部署容量。
- 后续：可观测门禁必须暴露拒绝计数、在途调用、pending 深度与 drain 时长，供容量和告警使用。
