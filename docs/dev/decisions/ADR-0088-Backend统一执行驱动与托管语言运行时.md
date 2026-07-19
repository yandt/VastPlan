# ADR-0088 Backend 统一执行驱动与托管语言运行时

- 状态：已采纳
- 日期：2026-07-20
- 关联：[ADR-0047 多语言运行驱动](ADR-0047-多语言运行驱动与第三方隔离边界.md)、[ADR-0048 发布者级运行策略](ADR-0048-发布者级插件运行策略.md)、[ADR-0051 Backend 混合插件运行](ADR-0051-Backend混合插件运行与受控内嵌边界.md)

## 背景

现有 `PluginRuntimeDriver` 只把已安装插件转换为 `LaunchSpec`，实际职责是生成子进程命令；`dynamic-go` 则在驱动注册表之外由 `PlacementPolicy` 直接调用。继续按这种结构增加 Node Worker、Python 子解释器、WASM 和容器，会让每种执行形态分别实现启动、身份核验、生命周期和回退逻辑。

Go 标准库 `plugin` 不能卸载，且要求宿主和 `.so` 共同构建，因此不能作为通用热替换底座。Node Worker 可以整体终止一个 JavaScript 执行环境；CPython 3.14 子解释器可以销毁独立解释器，但两者都不是恶意代码安全边界。系统还必须保留独立进程、容器和 WASM，以承载异构语言及第三方插件。

## 决策

1. Backend 控制内核继续使用 Go；本 ADR 不触发四类内核的整体语言迁移。语言运行环境通过执行驱动接入，协议契约继续以 JSON Schema、Protobuf 和 WIT 方向保持语言无关。
2. `PluginRuntimeDriver` 由 `PluginExecutionDriver` 取代。驱动不再只返回命令，而是接收可信 `LaunchPolicy`、启动插件并返回统一 `PluginInstance`。实例统一暴露身份、运行形态、进程标识、死亡信号、终结原因和停止能力。
3. 进程、托管运行时和内嵌实现共享同一条候选事务：准备候选、握手/声明核验、激活、状态迁移、注册路由、原子切换、Drain 旧实例、释放旧执行单元。Registry、权限、CallContext 投影、审计和迁移不得按语言复制。
4. 正式驱动名称为：
   - `native`：独立原生进程；
   - `python`：独立 Python 进程；
   - `node-worker`：由受信任 Node Runtime Host 在 Worker 中执行首方 ESM 插件；
   - `python-subinterpreter`：由受信任 Python Runtime Host 在 CPython 子解释器中执行首方插件；
   - `wasm-component`、`container`、`process-sandbox`：隔离驱动的稳定预留名；
   - `dynamic-go`：仅保留为首方历史承载，不再作为新插件的推荐形态。
5. Node Worker 插件必须声明 `execution.backend.node.workerSafe=true`，入口必须是 ESM，不能依赖主线程身份或调用 `process.exit()`。Worker 可终止和设置 V8 资源上限，但原生 Addon、全局 OOM 和宿主缺陷仍可能影响 Runtime Host，因此只提供 `trusted-runtime` 等级。
6. Python 子解释器插件必须声明 `execution.backend.python.subinterpreterSafe=true`。驱动要求 CPython 3.14+；插件及全部原生扩展必须支持多解释器。缺少声明、声明为 false 或运行时能力不足时 fail-closed，不静默切换执行形态。需要普通 Python 环境的插件明确使用 `driver=python`。
7. `node-worker`、`python-subinterpreter` 与 `dynamic-go` 不是第三方隔离手段，只允许经过节点发布者策略授权的可信插件。生产默认未知发布者至少需要 `process-sandbox`；`container` 与 `wasm-component` 也满足该下限。没有合格驱动时拒绝启动，不退回 `native/python/node-worker/python-subinterpreter/dynamic-go`。
8. Runtime Host 属于内核发布物和信任计算基，不是普通插件。它只接受宿主签发的一次性启动票据，不接受插件自行扩权；stdout/stderr、资源限制、健康和退出事实回到统一实例生命周期。驱动接口不约束 Runtime Host 采用每插件进程还是共享进程，后续可在不改变插件契约的前提下池化。
9. 热替换以“执行单元替换”实现，不依靠语言模块缓存技巧。新 Worker/解释器/进程先完全就绪，再切换路由并释放旧执行单元；有状态插件继续使用 `lifecycle.v1` 的 prepare/commit/rollback。
10. Python Runtime Host 的协议面固定在主解释器，业务入口才装入子解释器。跨解释器仅传输可复制的声明、裁剪后上下文、payload 与结果，避免要求 `grpcio`/Protobuf 原生扩展支持多解释器。桥接实现只能宣告已实现的协议 feature；第一版仅支持静态贡献与 Invoke，需要 HostCall、事件、动态贡献或迁移的插件必须明确使用独立 `python` 驱动。
11. 第三方隔离驱动采用“内核内固定等级 + 部署方可信 Runtime Host”注册：`VASTPLAN_PROCESS_SANDBOX_HOST`、`VASTPLAN_CONTAINER_HOST`、`VASTPLAN_WASM_COMPONENT_HOST` 只接受运维配置，不来自插件清单。未配置时对应驱动不注册；配置后内核以无 shell argv 传递已验签插件 ID、安装根、入口和参数，并继续持有一次性启动票据与协议授权。外部 Host 必须是内核发布物或部署方审计制品，不能把普通解释器/进程启动器冒充隔离 Host。

## 备选方案

- 全部 Backend 改为 Node.js：Worker 模型和 TypeScript 生态有优势，但会一次性重写已经封板的控制面、集群和供应链边界，当前否决；保留为执行内核实测后的未来决策。
- 全部 Backend 改为 Python：子解释器生态和原生扩展兼容度尚不足，且控制面没有依赖 Python AI 生态，否决。
- 在 Go 进程内同时嵌入 V8 与 CPython：会把 CGO/C++、两个 GC 和原生扩展故障域全部引入控制内核，否决。
- 继续为每种语言只生成 `LaunchSpec`：无法表达 Worker、子解释器和 WASM 实例的统一释放语义，否决。
- 第三方插件允许 Node `vm` 或 Python 子解释器：二者都不是安全边界，否决。

## 影响

- 正面：新增语言和执行形态不再修改 Runtime 主流程；热替换、迁移、监控和权限只有一套实现。
- 正面：Node/Python 第一方插件获得比 `dynamic-go` 更清晰的可释放执行单元，同时继续支持独立进程。
- 正面：第三方默认隔离由驱动能力和发布者策略共同强制，插件清单不能把自己声明为可信运行时。
- 代价：需要维护 Node/Python Runtime Host、SDK、运行时版本探测和跨语言 E2E。
- 代价：Worker/子解释器的调用需要消息复制，性能必须用真实负载衡量；不能把“同一 OS 进程”自动视为零成本。
- 边界：`trusted-runtime` 只降低生命周期和装载成本，不提供安全隔离。第三方仍必须使用进程沙箱、容器或 WASM。
