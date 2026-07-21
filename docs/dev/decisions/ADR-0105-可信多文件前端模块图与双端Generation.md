# ADR-0105 可信多文件前端模块图与双端 Generation

- 状态：已采纳，实施中
- 日期：2026-07-21
- 关联：[ADR-0062](ADR-0062-Frontend可信ESM制品与运行描述.md)、[ADR-0066](ADR-0066-Arco按需构建与单文件制品边界.md)、[ADR-0073](ADR-0073-Portal内容寻址交付快照.md)、[ADR-0078](ADR-0078-Frontend事务式热替换与插件生命周期.md)、[ADR-0097](ADR-0097-测试制品仓库与前端分级热升级.md)

## 背景

单文件 ESM 在可信加载协议初期简化了摘要验证，但迫使每个插件把页面、Renderer、语言资源和可选功能合并成一个文件。随着 Workbench Pattern、按需 Renderer 和 SSR 增加，单文件会放大首屏下载、重建和热替换成本。浏览器原生 ESM 的安全需求是完整闭包被锁定并逐文件复核，不是必须只有一个文件。

## 决策

1. 插件仍以不可变 `id/version/channel/packageDigest` 为构建、签名、发布、激活和回滚单位；传输和缓存可以按模块文件与 Chunk 进行。
2. 签名 Manifest 指向规范化 Module Graph。Graph 分 `browser` 和可选 `server` 两面，每个节点锁定包内路径、SHA-256、静态依赖和用途；入口必须属于图且全部依赖必须闭合。
3. Graph 禁止外部 URL、绝对路径、父目录、动态未声明导入、循环之外的未知节点和重复摘要映射。大小、文件数、单文件、深度和总解压量均受限。
4. Go Artifact Trust 服务验证包、发布者、Manifest 与 Graph 绑定；Node Portal Kernel 只物化密封结果并对实际字节再次复核。浏览器仍只读取当前 Activation 授权的同源内容寻址 URL，并在执行前复算摘要。
5. RuntimeSpec 锁定 Engine、Browser Graph、Server Graph、插件来源和精确 Generation。未选择 Renderer、路由或 Pattern 的文件不得预载或执行。
6. Browser 与 Server 候选由一个 Generation 事务协调：两端全部准备、健康和恢复成功后才提交；任一失败保留旧代。旧 Browser/Worker 在提交后 drain/dispose。
7. 生产禁止运行时 npm install、源码编译和任意包解析。所有 JS 都必须在发布前构建并进入签名制品。

## 备选方案

- **继续单文件**：安全简单，但无法满足细粒度下载和 SSR 图；仅作为迁移输入兼容，不是最终格式。
- **浏览器直接使用 npm/远端 import map**：绕过 VastPlan 制品信任、Activation 和撤销；拒绝。
- **只验证入口文件**：入口可加载未锁定依赖，完整性链断裂；拒绝。

## 影响

- 构建器、仓库、Catalog、RuntimeSpec、浏览器 Loader 和 Node Worker 必须共同消费同一 Graph 契约。
- 内容寻址对象数量增加，需要复用 ADR-0100 的引用与 GC 语义。
- 单插件小改动仍生成新插件版本，但客户端只需下载新的可达文件。

## 实施进展（2026-07-22）

- 已完成 browser/server Module Graph Schema、Go/Node 规范化 digest、esbuild 图生成、打包时 Manifest 注入及 Artifact Trust 对包内实际字节的二次绑定。
- 已完成 Go Catalog 对 Browser/Server Graph 的逐节点物化、摘要复核与不可变对象写入。Browser Graph 投影到公开 RuntimeSpec；Server Graph 只进入同一 revision 的密封交付区，使用 `server-object:` 私有定位符，浏览器对象 API 无法读取。
- Node Portal Kernel 冷预取会在提交本地 snapshot 前同时复核 browser/server 两面的全部对象；公开 Runtime 与私有 Server Runtime 使用分离读取端口，服务端代码不会进入 `/v1/portal-runtime` 或 `/v1/portal-modules`。
- 已完成 Browser Loader 对完整 DAG 的并行下载、摘要复算、响应绑定、受控 externals、依赖闭包、循环与 64 层深度拒绝、64 MiB 总量限制、相对 Chunk Blob URL 重写及入口导入。
- 每个 Portal Generation 独占并释放其 Blob URL；候选装配失败、预检结束、替换旧代和关闭时均执行清理。
- React Runtime Engine 已声明并构建首个签名 `frontendServer` 入口。专用 Server 构建器通过 `createRequire` 仅桥接签名图允许的 Node 内置模块，构建后测试必须真实 import 并执行 React SSR，不能只检查静态图。
- Server Worker 已实现 prepare、健康 render、原子提交、在途请求 drain、dispose、超时终止与内存/栈上限。SSR 结果限制为 1 MiB 并拒绝脚本或逃逸声明式 Shadow DOM 的 `</template>`；浏览器只 hydration 同一启动视图。当前双端提交的 Server 一面已完成，Browser 功能 Generation 继续沿用既有事务管理器；Host Epoch 仍协调跨端不兼容升级。

### 剩余实施项

- ADR 第 6 条要求 Browser 与 Server 候选由一个 Generation 事务协调。当前两端分别具有安全的候选准备、提交和失败保留机制，但尚无同一提交协调器：Server 在 Node Portal Kernel 内按首次 SSR 请求准备并提交，Browser 在页面内通过 `PortalGenerationManager` 准备并提交，Host Epoch 只处理不兼容边界。
- 在建立跨进程的 prepare token、Browser ready/commit acknowledgement、超时与旧代统一 drain 之前，本 ADR 保持“实施中”。不得把“两端各自原子”描述成“双端原子”，也不得为了状态收敛而弱化原决策。
