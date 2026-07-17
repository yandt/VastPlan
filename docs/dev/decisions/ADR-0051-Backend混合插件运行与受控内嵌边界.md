# ADR-0051 Backend 混合插件运行与受控内嵌边界

- 状态：已采纳
- 日期：2026-07-17

## 背景

ADR-0004 为 Backend 选择“独立进程 + 协议总线”，获得故障隔离、多语言和热装能力。部分极轻量、无状态、高频的第一方基础插件也支付进程切换和序列化成本；但若把“第一方可信”直接等同于“进程内运行”，会扩大内核故障域、模糊微内核边界，并让远端制品有机会请求进入内核进程。

Go 同时支持整体编译和标准库 `plugin` 动态加载。需要在不削弱默认隔离、不复制插件业务逻辑、也不绕过 Registry/权限/钩子/生命周期的前提下，允许内核使用者为少数第一方插件选择静态或动态内嵌。

## 方案比较

- **方案 A：全部独立进程**。边界最简单、隔离最好，但轻量高频插件仍有固定 IPC 成本。
- **方案 B：全部第一方插件进程内运行**。平均调用成本低，但第一方代码缺陷会直接影响内核，热装和多语言能力显著退化。
- **方案 C：默认进程 + 受控静态/dynamic-go 内嵌**。保留独立进程为默认；极少数基础插件可编译进发布物，其他第一方 Go 插件可从已签名制品加载 `.so`。

## 决策

采用方案 C，并修订 ADR-0004 在 Backend 上的绝对表述；ADR-0004 仍是默认运行形态和第三方插件边界。

### 1. 两个独立策略轴

1. `ExecutionPolicy` 决定发布者是否可运行以及最低隔离等级；签名清单 `minimumIsolation` 只能提高下限。
2. `PlacementPolicy` 决定运行形态，支持 `process-only / prefer-embedded / require-embedded / prefer-dynamic-go / require-dynamic-go`；精确插件规则优先于发布者规则，发布者规则优先于全局规则，零值与生产默认均为 `process-only`。

`prefer-embedded` 按“静态目录 → dynamic-go → 合规进程”选择；`require-embedded` 不允许最终回退进程。两个 dynamic-go 专用值跳过静态目录。`prefer-*` 在平台不支持、共同构建不匹配或 loader 不可用时回退已验签的进程入口，`require-*` 对同类问题 fail-closed；代码定义与验签贡献不一致属于发布漂移，即便是 `prefer-*` 也不静默降级。`allow-trusted` 不自动授予内嵌权限；要求 `process-sandbox/container/wasm` 的插件绝不能进入内核进程。

### 2. 所有内嵌实例的共同准入门

1. 插件必须同时满足 `publisher=vastplan` 和已分类的 `com.vastplan.<layer>.<category...>.<component>` 首方命名空间。运行配置不能把第三方发布者提升为可内嵌身份。
2. 部署方必须通过 `PlacementPolicy` 明确选择内嵌；Manifest 只能声明制品内容，不能自行取得内嵌权限。
3. 已验签、已安装的 LaunchPolicy 与代码声明的 ID、版本、扩展点、贡献 ID、优先级和 descriptor 必须逐项完全一致，有效隔离下限不得高于 `trusted-process`。

### 3. 静态内嵌

静态插件以 `ID + version` 精确存在于 Backend 二进制的 `EmbeddedCatalog`。具体插件只在 `composition/backendplugins` 发布物组合层登记，通用 `kernels/backend` 仍不 import 任何具体插件；升级静态插件必须发布新的 Backend 二进制。

### 4. dynamic-go 内嵌

Manifest 可在 `execution.backend.dynamicGo` 声明已签名制品中的 `.so` 路径、固定 ABI `vastplan.dynamic-go.v1` 与共同构建 `fingerprint`。打包阶段计算并写入指纹，随后整个 Manifest 与制品一起签名；安装器把入口和指纹冻结到内容寻址目录。加载器必须在 `plugin.Open` 前先比对已验签指纹，因为 `plugin.Open` 可能执行模块 `init()`；通过后才查找窄导出函数 `VastPlanDynamicGo`，取得与静态目录相同的 `EmbeddedPlugin` 定义。

dynamic-go 还必须满足：

- 仅 Linux、FreeBSD、macOS 且 `CGO_ENABLED=1`；其他构建保留进程能力并对 dynamic-go fail-closed。
- Backend 和 `.so` 使用同一 Go 工具链、目标平台、CGO/race 等关键参数，共享外部依赖版本一致。
- 官方共同构建把同一 SHA-256 构建指纹注入 Backend、`.so` 和待签名 Manifest；指纹覆盖工具链、平台、`go.mod/go.sum`、Git revision、工作区差异和 build tags。签名清单指纹在打开模块前校验，模块导出指纹在打开后复核；空值或不一致都拒绝加载。
- 标准库 `plugin` 不能卸载且 race detector 支持有限。同一 Backend 进程不得以不同版本或路径热替换已加载插件；升级必须滚动重启 Backend。内核候选切换不能冒充代码卸载。

`.so` 字节仍属于远端签名制品，必须经过既有 SHA-256、发布者证明、安装路径和清单授权链。构建指纹用于 ABI/源码协同，不替代制品签名。

### 5. 统一调用与生命周期

进程实例和两类内嵌实例共同使用 `PluginProcess` 兼容句柄、同一个 Registry、公开 `Host.Invoke` 安全管道、调用深度、资源限制、可观测、Drain、状态迁移和故障事件。差异只在最后一跳：进程实例走 gRPC Channel，内嵌实例调用 Go handler。

内嵌 handler 回调宿主时，宿主忽略其传入的 Caller/Principal，基于原始调用上下文重新签发当前插件身份；未在签名清单声明的 kernel service 继续拒绝。回调句柄只在当前 handler 调用期间有效。

### 6. 故障边界

可恢复的 Go `panic` 会转换成稳定应用错误、立即摘除该插件全部贡献并产生实例退出信号。内嵌代码导致的死循环、数据竞争、内存破坏、`fatal` 或 OOM 无法像独立进程一样隔离；这类风险由首方硬门禁、代码审查、测试和 Backend 多副本承接。第三方插件无论发布者运行策略如何配置，都不得静态或 dynamic-go 内嵌。

### 7. 首个试点

`com.vastplan.foundation.security.bootstrap-policy@0.1.0` 同时提供独立进程入口、静态定义和 dynamic-go `.so` 入口。三种承载共用运行时无关策略包及同一内嵌适配，避免权限逻辑或 descriptor 漂移。默认仍以进程运行。

## 影响

正面影响：高频基础插件可获得最低调用开销；其他首方 Go 插件无需重编内核即可受控动态内嵌；微内核通用包不依赖具体插件；部署方保留最终决策。

负面影响：内嵌故障域大于进程插件；静态版本升级需要新内核；dynamic-go 要求原生 CGO 构建、严格共同构建且不能卸载或进程内热升级。性能收益必须由基准和真实负载验证，不能仅凭“同进程更快”扩大内嵌范围。
