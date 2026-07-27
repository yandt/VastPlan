# ADR-0162 Workbench 拖拽内核与延迟 Dashboard 布局

- 状态：已采纳，基础能力已实施
- 日期：2026-07-27
- 关联：[ADR-0082](ADR-0082-前端工作台组合框架.md)、[ADR-0104](ADR-0104-Frontend-Runtime-Engine与React单实现.md)、[ADR-0105](ADR-0105-可信多文件前端模块图与双端Generation.md)、[ADR-0149](ADR-0149-源码职责规模门禁与渐进拆分.md)

## 背景

Workbench 的列设置已经需要排序，未来首页、看板、表单设计器和卡片工作区还会需要跨容器拖拽与响应式布局。继续使用原生 HTML5 DragEvent 会让触摸、键盘、自动滚动、碰撞反馈和虚拟化策略在每个 Pattern 中重复；让功能插件直接选择 dnd-kit、React DnD 或 Grid Layout，又会造成多个 Provider、事件模型和依赖版本。

首页卡片布局还包含拖动、缩放、断点布局和用户偏好，但当前尚未建立首页卡片注册与服务端偏好事务。若现在把完整 Dashboard 强行加入 Portal 页面注册，会把“布局引擎准备”与“首页产品设计”耦合。

## 决策

1. 通用拖拽基础采用精确锁定的 `@dnd-kit/react@0.5.0` 与 `@dnd-kit/helpers@0.5.0`。它们只允许出现在 Foundation Workbench 的 `patterns/interaction` 内部；Collection、未来 Kanban 等 Pattern 只消费稳定 ID、排序结果和渲染状态，不接触第三方事件对象。
2. 当前列偏好迁移到统一 `SortableList`。指针和触摸由 dnd-kit sensor 管理，键盘仍保留明确的 ArrowUp/ArrowDown 等价入口；排序状态始终由 Workbench 拥有并即时写入既有偏好端口。
3. 响应式卡片布局采用精确锁定的 `react-grid-layout@2.2.3`，但不新建独立插件。它作为同一 `ui.workflow.workbench` 内的 Dashboard Pattern Library，通过 `loadDashboardGrid()` 动态导入，仅在可信宿主实际请求 Dashboard 时下载和执行。
4. `DashboardGridSpec` 是 JSON-only 契约：只包含稳定卡片 ID、断点布局、列数、间距、行高和压缩方式。`defineDashboardGrid()` 对卡片重复、未知引用、越界、尺寸冲突和断点顺序做同步验证。功能插件不得在布局契约中传入 ReactNode、HTML、CSS 或第三方 Grid 属性。
5. 卡片 React 内容只能由可信 Dashboard 宿主根据已注册的语义卡片 ID 解析，再通过非序列化运行边界传入。拖拽结果只是新的布局候选；未来服务端用户偏好仍需 revision/CAS、权限校验和失败回滚，浏览器布局不是授权或持久化真相。
6. dnd-kit 与 Dashboard Grid 依赖都必须留在浏览器 Module Graph 的动态 Chunk。生产构建检查入口不得包含 dnd-kit、`react-grid-layout`、`react-draggable` 或 `react-resizable`；拖拽与 Dashboard 延迟 Chunk 预算分别为 200,000 和 250,000 字节。
7. 本 ADR 只交付 Dashboard 布局引擎、契约和延迟装载边界，不宣称首页已完成。正式首页启用前还必须补齐卡片贡献目录、用户布局偏好事务、角色裁剪、键盘移动/缩放、窄屏降级和真实 Gallery/QA。

## 语言与运行方式

选择 TypeScript + React，并运行在既有 React Frontend Runtime Engine 中。dnd-kit 和 react-grid-layout 都直接依赖浏览器 DOM、React 生命周期与前端布局测量；Go、Python 和后端 Node.js 在该交互层没有生态或执行优势，也不应新增独立进程。第三方库不成为插件协议，只是 Workbench 制品内部可替换实现。

## 备选方案

- **Pragmatic Drag and Drop 作为底座**：更小且框架无关，但需要自行实现更多排序、键盘提示和状态反馈；保留为 dnd-kit 不能满足虚拟化或框架演进时的替换候选。
- **每种 Pattern 自选拖拽库**：短期交付快，但会复制 Provider、无障碍与热加载边界，拒绝。
- **把 react-grid-layout 静态打入 Workbench 入口**：实现最简单，却会让所有管理页面承担未来首页的下载成本，拒绝。
- **为 Dashboard 再建基础插件**：会在尚无独立装配和生命周期需求时继续细分插件，拒绝；先作为 Workbench 内部按需 Library。

## 影响

- 列排序获得统一的 pointer/touch/keyboard 基础，后续列表与看板不再直接使用原生 DragEvent。
- 未来首页可以直接消费受治理 Dashboard Grid，而不必改变 Renderer 或让功能插件组合 React 组件。
- 新版 dnd-kit 尚未到 1.0；精确版本、内部封装和测试隔离了接口变化，升级只修改 Foundation Workbench。
- dnd-kit 与 Dashboard 相关依赖均保持延迟加载，普通 Workbench 页面不承担其下载成本。生产构建和 Module Graph 测试持续验证该边界。
