# ADR-0140：后台服务自主 HostCall 与宿主绑定上下文

- 状态：已采纳
- 日期：2026-07-24
- 关联：[ADR-0061](ADR-0061-统一调用信封与受众投影.md)、[ADR-0128](ADR-0128-统一Leader-Epoch与外部副作用Fencing.md)、[ADR-0139](ADR-0139-安全评估Provider与持续复扫控制器.md)

## 背景

普通插件只在处理宿主 `Invoke` 时发起 `HostCall`，宿主为该次调用签发短生命周期 delegation token，并用它重建可信 `CallContext`。定时控制器、租约维护器等后台服务没有入站调用；若沿用空 token，它们会被正确拒绝。给单个 Controller 写例外、让插件自报 tenant/principal，或签发长期 bearer 都会破坏统一协议与最小权限边界。

## 决策

1. 签名 Manifest 的 `runtime.backgroundService=true` 显式声明“激活后可在没有入站调用时自主 HostCall”。首版只允许 `leader + leader-owned + leader`，要求 `service + restart` 配置，并要求配置 Schema 中存在必填字符串 `tenantId`。
2. 安装器把声明冻结进 `PluginRuntimeContract`。Node Agent 从已经过配置 Schema 校验、且隔离到该插件的服务配置读取 `tenantId`，写入 host-only `LaunchPolicy.AutonomousTenantID`；插件进程不能从握手或消息修改它。能力与租户必须同时存在，否则启动 fail-closed。
3. 宿主仅在 `ACTIVATE` 成功后开放自主调用，在发送 `DRAIN/SHUTDOWN` 前以及会话 teardown 时立即关闭。普通插件、未激活插件和已排空插件仍必须提供有效 invocation delegation。
4. 自主调用不签发长期 token。宿主忽略插件自报的 principal、caller、credentials、metadata、project、trace 和 call path，只构造 `{tenantId, scene=system.background, caller=plugin}`。插件显式携带与绑定值不同的 tenant 时拒绝调用。
5. Manifest 声明的 capability、kernel service 和发布者上限继续生效；该能力只解决“调用从哪里开始”，不扩大“可以调用什么”。Leader 丢失 execution fence 后，Runtime Host 继续阻止调用离开本服务，Shared State fenced mutation 与下游 CAS/只追加规则仍是最终竞态防线。
6. `dynamic-go` 首版不得声明后台服务。其 Host handle 只在当前处理器调用期间有效；把长生命周期回调暴露到内核进程会扩大内嵌故障面。独立进程和支持完整 HostCall 的托管语言 Runtime 可复用同一 wire 语义，但本 ADR 首个消费者使用 Go 独立进程。

## 否决方案

- **后台任务借 `status`/poll 调用触发**：把调度正确性绑到管理流量并隐藏生命周期，否决。
- **会话级长期 delegation token**：泄露后直到进程退出都可重放，且容易携带过宽 principal，否决。
- **信任插件回传 CallContext**：插件可跨租户或伪造管理员、凭证和调用路径，否决。
- **按插件 ID 在 Host 中硬编码特例**：后续每个控制器复制安全逻辑，否决。

## 实施记录

- 2026-07-24：Schema、Go Manifest 类型、安装冻结契约、Node Agent tenant 绑定、Protocolbus 生命周期门控与安全测试落地；首个使用者为制品持续复扫 Controller。
