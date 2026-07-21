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

## 实施进展（2026-07-21）

- 已完成 browser/server Module Graph Schema、Go/Node 规范化 digest、esbuild 图生成、打包时 Manifest 注入及 Artifact Trust 对包内实际字节的二次绑定。
- 已完成 Go Catalog 对 Browser Graph 的物化、逐节点内容寻址交付、RuntimeSpec 投影、恢复版本 URL 和媒体类型响应；Server Graph 仍只保留在可信服务端边界。
- 已完成 Browser Loader 对完整 DAG 的并行下载、摘要复算、响应绑定、受控 externals、依赖闭包、循环与 64 层深度拒绝、64 MiB 总量限制、相对 Chunk Blob URL 重写及入口导入。
- 每个 Portal Generation 独占并释放其 Blob URL；候选装配失败、预检结束、替换旧代和关闭时均执行清理。
- 尚未完成双端 Generation 的 Server Worker/SSR 一面；该部分与 Node Portal Kernel 的 Addressing/BFF 接管继续按 ADR-0103 实施。
