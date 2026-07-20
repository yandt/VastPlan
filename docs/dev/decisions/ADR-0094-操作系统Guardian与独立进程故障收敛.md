# ADR-0094 操作系统 Guardian 与独立进程故障收敛

- 状态：已采纳
- 日期：2026-07-20
- 关联：[ADR-0032 Backend 插件生命周期与实际态 v2](ADR-0032-Backend插件生命周期实际态v2.md)、[ADR-0035 Backend 可观测与健康契约](ADR-0035-Backend可观测与健康契约.md)、[ADR-0069 Linux SSH 首次引导与 Node Agent 接管](ADR-0069-SSH首次引导与Node-Agent接管.md)、[ADR-0089 Runtime Provider 与共享 Host 池](ADR-0089-Runtime-Provider与共享Host池.md)

## 背景

Backend Kernel 会直接启动独立插件进程，也会启动承载多个逻辑插件的语言 Runtime Host。既有协议总线每 5 秒发送应用层 ping，连续 15 秒无响应会撤销 session；但单靠协议心跳不能解决父进程被强杀后子孙进程遗留、插件派生进程未被一并清理，以及 Kernel 控制循环卡死但进程仍存在等操作系统级故障。

为每个插件再套一层常驻 Supervisor 壳能够处理部分问题，却会增加进程数量、启动协议、日志归属和新的自监督循环。生产环境已经以 systemd 托管 Node Agent，内核也已经拥有 Runtime Manager，因此不应再创建一套重叠的进程编排系统。

## 决策

采用“服务管理器监督 Kernel、Kernel Guardian 监督物理子进程、协议心跳监督逻辑执行单元”的三层故障收敛模型：

1. Runtime Manager 继续拥有物理进程生命周期。`ProcessGuardian` 是内核固有、可测试注入的操作系统适配器，不是普通插件，也不为每个子进程启动 Supervisor 壳。
2. 独立插件和共享 Runtime Host 的所有 `exec` 入口必须先经过同一 Guardian；禁止业务驱动绕开它自行启动长期进程。
3. Linux 为直接子进程创建独立进程组并设置 `PDEATHSIG=SIGTERM`。Kernel 正常关闭时先走插件 lifecycle 或 Runtime Host `shutdown`，再向整个进程组发送 `SIGTERM`，宽限 5 秒后发送 `SIGKILL`。这会同时清理插件派生的子孙进程。
4. macOS/BSD 使用独立进程组和组信号；其他平台保留显式回退。Windows Job Object、容器 init/cgroup 等平台 Guardian 可在不改变 Runtime Manager 的前提下追加。
5. 多个插件或 Runtime Host 并行关闭，使总停机时间由最慢的一个执行单元决定，不能把每个故障进程的宽限期串行累加。
6. Linux 生产 unit 使用 `Type=notify`。Node Agent 完成控制面连接和 Node Lease Guard 后发送 `READY=1`；退出前发送 `STOPPING=1`。
7. systemd `WatchdogSec=60s`。独立通知 goroutine 只有在 Agent/Reconciler 控制循环持续推进时才能发送 `WATCHDOG=1`，不能用“喂狗线程仍在运行”掩盖调度循环卡死。单次 reconcile 使用与 context deadline 相同的 15 分钟有界工作租约，允许大制品下载和候选启动；租约到期后不能继续喂狗。
8. unit 显式使用 `KillMode=control-group`、`SendSIGKILL=yes`、`Restart=on-failure` 和 90 秒停机上限。Kernel 崩溃或 watchdog 超时时，systemd 清理整个 unit cgroup 后再重启，不允许旧插件进程与新 Kernel 并存。
9. 既有协议总线 5 秒/15 秒心跳继续检测插件和 Runtime Host 内逻辑单元的应用层活性；不再新增 Supervisor 与 Kernel 的第二套双向心跳。进程退出、协议失活和 Node Agent 卡死必须记录为不同故障原因。
10. 非 systemd 本地开发仍受 Guardian 保护但没有服务级自动重启。只有在明确需要裸机、无 service manager 部署时，才评估可选的单实例轻量 shim；它不是默认架构。

## 备选方案

- **每个插件一个通用 Supervisor 壳**：可移植，但进程数和协议复杂度随插件增长，并产生“谁监督 Supervisor”的递归问题，否决为默认。
- **只依赖 systemd**：能回收同一 unit cgroup，却无法表达开发环境、逐插件 lifecycle 和共享 Runtime Host 逻辑单元，否决。
- **只依赖协议心跳**：可发现应用层卡死，但父进程死亡时没有发送心跳或清理子孙进程的主体，否决。
- **Kernel 与 Supervisor 双向心跳**：信息与 systemd watchdog、协议 ping 重叠，还引入脑裂判定，否决。

## 当前实现

- `processguard` 已接入独立插件与共享 Runtime Host 的唯一启动和回收路径；Linux 使用进程组与父进程死亡信号，Unix 使用进程组回收。
- 协议 Host 和 Runtime Pool 已并行关闭多个执行单元。
- Node Agent/Reconciler 已提供真实进展脉冲，`servicewatchdog` 据此发送 systemd `READY/STOPPING/WATCHDOG`。
- SSH 首次引导生成的 unit 已采用 notify、watchdog、control-group 清理和失败重启策略。
- macOS 已覆盖进程组清理测试；Linux `PDEATHSIG` 故障测试纳入 Linux 构建。Windows Job Object 与容器编排专用 Guardian 留待对应部署目标出现时实现。

## 影响

- Kernel 被关闭、强杀或卡死后，插件进程不会成为不受管的后台服务；共享 Host 内多个逻辑插件仍由协议层分别判活。
- 生产故障恢复复用 systemd，不增加每插件常驻进程，也避免自监督递归。
- `PDEATHSIG` 只是一层快速收敛保护，最终清理由 systemd cgroup 和显式进程组信号共同保证；不能把它单独当成完整服务管理器。
