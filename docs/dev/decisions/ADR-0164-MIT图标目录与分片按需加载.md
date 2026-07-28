# ADR-0164 MIT 图标目录与分片按需加载

- 状态：已采纳并实施
- 日期：2026-07-28
- 关联：[ADR-0111](ADR-0111-语义图标与Workbench页面动作宿主.md)、[ADR-0159](ADR-0159-Ant-Design首选Renderer与按需交付.md)、[ADR-0163](ADR-0163-Workbench动作数据契约与图标化操作面.md)

## 背景

UI Contract 已用 `SemanticIconName` 约束功能插件，但 canonical 基线只有 23 个手工 SVG，新增语义需要重复绘制和维护。曾评估 Iconfont 的“阿里云控制台官方图标库”，其现行协议没有授予开源仓库再分发权，并明确限制把素材重新发布到第三方平台，因此不能进入 Apache-2.0 仓库。

项目已经锁定 Ant Design Icons。其抽象 SVG 包 `@ant-design/icons-svg@4.5.0` 声明 MIT，共有 846 个图标定义，适合作为可审计的完整图标源。但若每个图标生成独立网络文件，会超过 Node Portal Kernel 的 512 个静态资产安全上限，也会增加启动扫描成本。

## 决策

1. 新增 TypeScript SDK `@vastplan/icon-catalog`，从精确锁定的 `@ant-design/icons-svg@4.5.0` 构建完整目录。它是无独立生命周期、配置和远程能力的共享资源库，不为了代码复用再拆成插件。
2. 保留两层名称：846 个 `IconCatalogName` 只用于 Foundation UI、图标浏览器和语义提升工具；功能插件、Workbench、Shell 和动作契约仍只能使用稳定 `SemanticIconName`。源码构建门禁和浏览器 Module Graph 复验都拒绝非 `cn.vastplan.foundation.frontend.*` 模块直接导入原始目录。
3. `canonical` 的现有 23 个语义图标映射到 Ant Design Outlined 定义，并通过独立 `@vastplan/icon-catalog/semantic` 入口同步交付。Arco、MUI 和 Ant Design Renderer 在 canonical 模式下使用同一几何；`renderer-native` 仍由各 Renderer 自己映射。
4. 完整目录按图标名称的 FNV-1a 摘要稳定分到 27 个逻辑分片，每个分片约 20–44 个图标。完整目录入口和分片都不进入 Portal 初始静态依赖闭包；首次请求某个原始图标时才下载对应分片，同分片后续请求复用 Promise 与 ESM 模块缓存。
5. 不提高 Portal 静态资产文件数上限。esbuild 可以抽取共享块，但构建门禁要求：完整 846 个定义存在、逻辑延迟分片恰为 27、语义入口只静态包含 23 个定义、目录入口不静态内联 SVG、总输出文件不超过 96、语义闭包不超过 96 KiB。
6. 上游抽象节点必须先进入白名单归一化器：只保留 `path` 与安全 `g.transform`，拒绝未知节点、非法 path、fill、opacity、fill-rule、viewBox 和 transform。`defs/style/filter` 不进入运行树；TwoTone 次级图形使用 `currentColor` 的 35% 透明度，使主题颜色保持统一且不依赖内联样式或 SVG filter。
7. 仓库 `NOTICE` 和目录内 `THIRD_PARTY_LICENSE` 保留 Ant UED 归属与 MIT 文本。生成文件只保存模块引用、元数据和分片目录，不复制 Iconfont 素材。

## 备选方案

- 精选 266 个并逐图标分块：文件数可控，但人为裁剪完整 MIT 目录，未来仍需反复扩充，拒绝。
- 846 个图标逐个生成网络文件并提高静态资产上限：加载粒度最细，但为 UI 资源扩大内核安全上限并增加启动扫描，拒绝。
- 将全部 SVG 放入一个 JSON 或 JS：文件少，但任何一个图标都会下载整个目录，不满足按需加载，拒绝。
- 把图标目录做成安装插件：会为无运行生命周期的静态资源增加组合、版本和热替换复杂度，拒绝。

## 影响

- 功能插件的公共契约没有扩大到 846 个视觉名称；新增业务动作仍需先选择或提升准确语义。
- Portal 默认启动成本只增加精简的 23 图标语义闭包，完整目录只有图标管理类 Foundation 功能使用时才加载。
- 上游新增、删除或重命名图标会导致 846 数量、文件列表摘要或分片清单门禁失败，必须经显式依赖升级和 ADR/NOTICE 复核。
- Twitch 等依赖 SVG filter 的装饰效果会被安全归一化为主体路径；这是 canonical 一致性与 CSP 边界的有意取舍。
