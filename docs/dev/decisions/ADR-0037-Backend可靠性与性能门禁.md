# ADR-0037 Backend 可靠性与性能门禁

- 状态：已采纳
- 日期：2026-07-16
- 关联：[ADR-0031 Backend 内核 1.0 封板与工程门禁](ADR-0031-Backend内核1.0封板与工程门禁.md)

## 背景

普通单测通过不能证明解析器面对任意输入不崩溃，也不能发现每次改动增加少量分配或 goroutine 的累积退化。性能数字受硬件影响，提交一份在开发机测得的绝对 ns/op 基线会在 CI 制造假失败；短时测试也不能冒充 24 小时稳定性证据。

## 决策

1. 插件清单、运行 descriptor 和 Deployment v2 解析器进入 Go fuzz；CI 每入口执行短时 fuzz smoke，发布前可延长语料运行。
2. 故障证据使用真实进程/网络边界：已有 E2E 主动触发插件崩溃、在途断连、迁移 prepare/commit/rollback 失败、NATS watch 重连和候选注册失败，不新增只能命中 mock 的“故障开关”。
3. 核心 benchmark 覆盖 Registry lookup/fanout、协议 context/关联表、本地 addressing、调度筛选和 scoped persistence。PR 在同一 runner、同一次 job 中分别采样 base/head 各五次并比较中位数。
4. 耗时同时超过 1.5 倍和 100ns 绝对增量，或 B/op、allocs/op 超过 1.25 倍时阻断。新增 benchmark 首次无 base 对照，合入后成为后续基线。
5. `e2e && soak` 运行真实插件请求并周期重启会话。默认时长固定 24h，记录调用数、重启数、goroutine 和文件句柄起止/峰值，并检查 session pending。短时 smoke 不得登记为发布完成。

## 影响

- 正面：可靠性与性能退化都有可重复入口和机器门禁，不依赖个人命令历史。
- 代价：PR benchmark 增加 CI 时间；24h soak 需要单独 runner 配额。
- 风险：benchmark 仍会有噪声，因此使用中位数和双条件耗时阈值；阈值变化必须新增 ADR。
