# ADR-0089 Runtime Provider 与共享 Host 池

- 状态：已采纳
- 日期：2026-07-20
- 关联：[ADR-0004 插件运行形态](ADR-0004-插件运行形态.md)、[ADR-0047 多语言运行驱动](ADR-0047-多语言运行驱动与第三方隔离边界.md)、[ADR-0051 Backend 混合插件运行](ADR-0051-Backend混合插件运行与受控内嵌边界.md)、[ADR-0088 Backend 统一执行驱动](ADR-0088-Backend统一执行驱动与托管语言运行时.md)

## 背景

ADR-0088 统一了执行驱动接口，但首版 Node Worker 和 Python 子解释器仍是“每插件一个 Runtime Host 进程”。这能验证协议与语言能力，却把逻辑插件实例、语言执行单元和物理进程混为一个生命周期：插件越多，Host 进程越多；关闭任意插件也只能依赖杀死其 Host。dynamic-go 则直接在 Backend 进程执行 `plugin.Open`，无法卸载且扩大控制内核故障域。

系统需要同时满足：默认低进程数量、插件级独立生命周期、按安全域隔离、Host 崩溃可观测、候选整代热替换，以及部署方显式要求单插件进程的能力。

## 决策

1. 内核固有 `Runtime Manager` 负责 Runtime Provider 选择、Pool 分组、物理 Host 生命周期、健康和故障收敛。Runtime Manager 不是插件，不能被业务组合替换。
2. 语言或隔离实现以受信任 `Runtime Provider` 接入。Provider 是可签名、版本化、可发现的系统级发布物，但属于启动信任链，不是普通 application 插件，不能注册业务贡献或由插件清单自行替换。
3. 运行态拆为三个对象：
   - `RuntimeHostProcess`：物理进程或等价执行容器；
   - `PluginExecutionUnit`：Worker、子解释器、WASM Instance 或 Go 模块等逻辑执行单元；
   - `PluginInstance`：逐插件协议 session、贡献、授权与生命周期。
   `PluginInstance.PID` 可指向共享 Host，因此实际态 PID 必须去重。
4. Node Worker 与 Python 子解释器默认使用共享 Pool。一个 Pool 在当前阶段固定最多一个物理 Host，不自动扩容或静默溢出为独立进程。只有显式 `dedicated` 规则才创建单插件 Host。
5. Pool key 至少包含内核服务 scope、Provider、隔离等级、发布者信任域、平台及签名执行要求兼容摘要。`shared` 只能在该硬边界内共享；配置只能减少共享，不能跨越安全或 ABI 边界强制合并。
6. Host 模式优先级为精确插件 > 发布者 > 全局默认。生产默认是 `shared`；CLI 使用 `-runtime-hosting-default`、`-publisher-runtime-hosting`、`-plugin-runtime-hosting`。这些是节点策略，不进入插件 Manifest，避免插件自行改变故障域。
7. Runtime Host 只通过父子进程 stdin/stdout JSON 行控制通道接受 `start/stop/shutdown`。每个 start 仍由协议宿主签发独立的一次性票据和裁剪环境；Host 进程本身不继承 Backend 环境，不能把一个插件的票据或允许变量传播给同池其他单元。
8. 关闭插件先完成逐 session lifecycle，再停止逻辑执行单元和释放 lease。最后一个 lease 释放后异步回收物理 Host；回收不能阻塞拥有 gRPC stream 的 teardown，否则会形成“stream 等进程、进程等 stream”的循环等待。
9. 物理 Host 崩溃时，同池 session 会各自得到真实死亡信号、撤销贡献并触发既有 Reconciler 恢复。内核不自动把可疑插件升级为独立进程；是否 dedicated 仍由明确策略决定。
10. dynamic-go 迁入 Go Runtime Provider，Backend 不再直接 `plugin.Open`。Go Provider 按服务、完整 ABI/构建指纹和目标组合 generation 分池；升级创建新 Go Host generation，完全加载和校验后原子切路由，再排空旧 Host。第三方仍禁止 dynamic-go。
11. 四类内核共享 Provider、Pool key、实例/执行单元/Host、健康和代际切换语义，但不强求同一物理机制。Frontend 的 dedicated 可映射为 Worker/iframe，Mobile 受 OS 进程模型约束，Runner 编译型插件继续遵守整体签名 Bundle 决策。

## 当前实现

- Backend 已实现 `RuntimePoolManager`、共享/专用策略、Host lease、物理进程状态快照和空池回收。
- `protocolbus` 已分离受管执行单元停止回调与独立进程句柄，单 session teardown 不会杀死共享 Host。
- Node Runtime Host 支持多 Worker；Python Runtime Host 支持多 CPython 子解释器。两者均保持逐插件票据、session、贡献和停止能力。
- Node/Python 跨语言 E2E 验证两个插件共享同一 PID，关闭其中一个后另一个仍可调用。
- Go Runtime Host 已成为唯一允许调用 `plugin.Open` 的生产包；Backend 只签发逐实例票据、持有 session 并管理 generation-scoped lease。
- dynamic-go 的新制品指纹形成新 Pool generation；候选接入并切路由后释放旧 generation，Backend 本身无需因 Go 插件升级而重启。
- Node、Python 与 dynamic-go 跨运行时 E2E 分别验证共享 PID、逐实例关闭和 Backend 外加载边界。

## 备选方案

- 每插件一个进程：隔离直观，但进程数量与插件线性增长，否决为默认，保留 `dedicated`。
- 每种语言全节点唯一进程：进程最少，但会跨服务和信任域扩大故障与权限面，否决。
- 自动按负载无限扩容：真实插件负载尚未形成，阈值没有证据，当前否决；将来只在兼容 Pool 上受上限启用。
- 把 Runtime Manager 本身做成普通插件：会形成“由谁先加载运行时插件”的启动循环，否决。

## 影响

- 正面：多个首方插件共享语言进程，逻辑生命周期仍完全独立；实际态展示真实去重 PID。
- 正面：新增 Java/.NET/WASM 等 Provider 不需要复制 Reconciler、Registry、权限和迁移事务。
- 正面：发布者与 ABI 成为硬隔离键，减少误共享；部署方仍可逐发布者、逐插件选择 dedicated。
- 代价：共享 Host 崩溃会影响同池插件，需要确定性恢复、隔离诊断和未来 quarantine 策略。
- 代价：Runtime Host 管理通道、日志分流和关闭顺序成为可信基础设施，必须纳入跨语言 E2E 与发布门禁。
