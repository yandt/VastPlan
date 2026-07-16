# ADR-0035 Backend 可观测与健康契约

- 状态：已采纳
- 日期：2026-07-16
- 关联：[ADR-0031 Backend 内核 1.0 封板与工程门禁](ADR-0031-Backend内核1.0封板与工程门禁.md)、[ADR-0034 Backend 协议资源边界](ADR-0034-Backend协议资源边界.md)

## 背景

Backend 内核需要在不绑定日志平台、指标数据库或 tracing 厂商的前提下，给运维提供一致的结构化事件、调用链身份、资源指标和健康事实。只保留 `printf` 文本无法稳定采集字段；只定义 OpenTelemetry 依赖又会把具体 exporter 生命周期固化到微内核。

## 决策

1. Backend 进程以标准库 `slog` JSON handler 为统一日志出口；旧 `log.Fatal` 也经 `slog.SetDefault` 接管。日志不得写 payload、凭证明文或任意用户 metadata。
2. `shared/go/observability.Observer` 是内核窄接口：日志使用 `slog.Logger`，指标输出到可替换 `MetricSink`。默认 `MemoryMetrics` 提供有界诊断快照；部署适配器可替换为 OTel/Prometheus sink，内核不依赖 exporter。
3. 每次 Host 调用派生新 `span_id`，保留 `trace_id`，旧 span 成为 `parent_span_id`；派生发生在克隆的 `CallContext`，不修改调用方对象。调用完成记录固定低基数的状态计数和耗时。
4. `Host.Healthy` 表示协议服务仍存活；`Host.Ready` 在启动后且未 drain 时为真。`DiagnosticSnapshot` 原子汇总健康/就绪、drain、在途调用、会话 pending、资源上限与指标，不包含业务 payload。
5. 内置 `kernel.diagnostics` capability 通过正常权限管道返回同一快照；HTTP/Kubernetes probe 或 support bundle 由部署适配器调用公开方法实现，不在内核中绑定 Web 框架和端口。

## 备选方案

- **内核直接依赖完整 OTel SDK/exporter**：生态成熟，但把采集后端配置和生命周期固化进核心。拒绝。
- **继续传递 `func(format, args...)`**：兼容简单，但没有稳定字段、trace 与指标契约。只保留为旧组件适配入口，不再作为真源。
- **诊断快照包含最近 payload/metadata**：排障直观但会扩大敏感数据面。拒绝。

## 影响

- 正面：调用日志、trace、metric、health/readiness 与资源状态有统一、可测试的事实源。
- 代价：当前内存指标仅用于即时诊断，长期存储必须配置外部 sink。
- 风险：标签必须保持内核固定低基数，插件 ID、用户 ID 等高基数字段不得进入 metric key。
- 后续：发布运维门禁提供 probe 适配和 support bundle runbook；可靠性门禁对快照并发与 sink 故障做故障注入。
