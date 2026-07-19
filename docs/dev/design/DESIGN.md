# VastPlan Portal 设计系统

> 状态：设计基线 v2｜最后更新：2026-07-19
>
> 本文是 Portal 跨布局、跨 Arco/MUI 的视觉与交互单一真相源。组件职责和安全边界见《[前端门户内核](../architecture/前端门户内核.md)》，架构取舍见 [ADR-0080](../decisions/ADR-0080-Portal三级导航与可切换布局.md) 与 [ADR-0081](../decisions/ADR-0081-Portal治理与不可变Activation.md)。

## 1. 设计原则

1. Portal 是密集但可扫描的企业管理工作区，不使用营销式 Hero、卡片仪表盘、装饰渐变或无意义图标。
2. 每个区域只有一个任务。列表负责选择，详情负责理解，编辑器负责修改，Activation 流程负责上线。
3. 当前线上 Activation 是 Portal 详情的第一视觉锚点；草稿和 Published 输入不能伪装成线上状态。
4. 颜色不单独承载含义；活动、失败、警告和禁用同时使用形状、位置、文字或图标。
5. 区域宿主管理自己的溢出。结构化菜单可进入“更多”；任意插件内容只能按区域策略滚动、换行、截断或折叠。

## 2. 基础 Token

UI Contract 2.0 暴露语义 token，适配器映射到具体框架。布局插件不得读取 Arco/MUI 私有 token。列表、卡片、表单和操作区的一致性交给《[UI 工作台组合框架](../architecture/UI工作台组合框架.md)》；布局只决定它们所在区域的视觉位置。

| Token | 基线 | 用途 |
|---|---:|---|
| `shell.barHeight` | 64px | Logo、导航标题、Page Header 共同高度 |
| `shell.railWidth` | 64px | 标准布局图标轨 |
| `shell.navigationWidth` | 240px | 标准桌面导航面板 |
| `shell.navigationCompactWidth` | 220px | 768–1199px 导航面板 |
| `overlay.navigationMinWidth` | 480px | Mega Popover 最小宽度 |
| `overlay.navigationMaxWidth` | 840px | Mega Popover 最大宽度 |
| `motion.fast` | 120ms | hover、focus 与微小状态变化 |
| `motion.normal` | 180ms | Popover、Drawer、折叠章节 |
| `focus.width` | 2px | 键盘焦点环 |
| `touch.minimum` | 44px | 最小触控目标 |

颜色至少包含 `canvas / surface / overlaySurface / text / mutedText / border / primary / danger / warning / success / hover / selected / focusRing`；形状至少包含 `radius.sm/md/lg` 与 `elevation.overlay`。所有 token 必须在深浅主题满足 WCAG AA 正文对比度。

## 3. 排版

- 自托管 `Noto Sans` 与 `Noto Sans SC`，按 Unicode Range 分包并使用 `font-display: swap`。
- 只预加载 Latin 与常用中文子集，其余按页面实际字形加载；字体失败时回退到平台无衬线字体，但不得阻塞 Portal 启动。
- 正文视觉字号不小于 16px；辅助信息可使用 14px，但必须满足 4.5:1 对比度。
- Page Title 桌面为 22px，移动端为 20px；长标题单行省略，tooltip 和可访问名称保留全文。

## 4. Shell 与布局

### 4.1 公共区域

- `shell.header.*`、`shell.navigation.*`、`page.header.*`、`page.body.*`、`page.aside` 和 `shell.footer` 的拓扑由组合插件统一管理。
- Shell Header、Aside、Footer 等没有内建内容且全部 Slot 为空时不创建 DOM 和占位；Page Header 因承担页面定位始终存在。
- Page Header 位于 Page Body 滚动容器之外。正文滚动时保持可见，不依赖多层 `position: sticky`。

### 4.2 顶部导航

- 64px 顶栏：Logo 在 start；`primary` 和 `secondary` 根组在 center；`settings` 和账户区在 end。
- `primary` 与 `secondary` 之间有视觉分隔。活动根组同时显示 selected surface、位置标记和 `aria-expanded/aria-current` 关联状态。
- Mega Popover 宽度为 480–840px；子组使用 `repeat(auto-fit, minmax(220px, 1fr))`，最多三列。根直属页面横跨顶部。
- 只允许一个 Overlay；不使用 hover 打开、不使用嵌套 Popover。重新点击触发器关闭，切换触发器替换内容。
- 空间不足时尾部根组进入“更多”；活动根组优先留在顶栏。“更多”不改变导航树，只改变视觉承载。

### 4.3 标准侧栏

- 64px 图标轨 + 240px 常驻面板；中间宽度面板为 220px。
- 根直属页面在面板顶部；子组为可多开的折叠章节。活动子组自动展开，状态按根组保留在当前浏览器会话。
- 图标轨、面板和正文分别拥有独立纵向滚动边界。

### 4.4 响应式

- 使用 Shell 容器宽度：`≥1200px` 完整桌面；`768–1199px` 收窄并使用“更多”；`<768px` 使用全高 Drawer。
- 移动 Drawer 展示完整树并自动展开活动路径；关闭后焦点回到触发按钮。
- Page Header 在移动端可因 Slot 换行而增高；Page Body、Table 和 Overlay 各自管理窄屏溢出。

## 5. Overlay 与导航交互

- `Popover` 为受控语义组件，适配器拥有定位、碰撞翻转、外部点击、ESC、焦点恢复、Shadow DOM Portal 和 z-index。
- 顶部导航采用 disclosure pattern：Enter、Space、ArrowDown 打开；Escape 关闭；Left/Right/Home/End 在根组间移动。
- 打开后优先聚焦当前页面链接，否则聚焦第一个链接。页面保持正常链接语义并使用 `aria-current="page"`。
- 标准图标轨支持 Up/Down/Home/End；ArrowRight 进入面板，ArrowLeft 返回原根组。
- 折叠子组按钮使用 `aria-expanded`。所有交互支持键盘-only、读屏、RTL、200% 缩放和 reduced-motion。

## 6. 管理工作区

- 平台管理中心分为 `Platform Profiles` 与 `Portals` 两个工作区。
- 使用 master-detail 或详情路由，不采用资源卡片网格。
- Portal 详情顶部使用连续状态带展示当前 Activation、Profile、Application、Binding、健康状态与生效时间。
- Application、Profile、Binding 的 Published 状态只表示可被引用。Activation 才使用“准备中、激活中、当前生效、已取代、失败”。
- Activation 与回滚都使用独立全页流程：选择 → 校验/差异 → 确认 → 阶段进度 → 持久结果。
- 差异首先按布局、设计系统、插件、服务绑定、权限和路由展示语义摘要；原始 JSON 仅为可展开技术详情。

## 7. 状态与反馈

- 首次加载使用与最终结构一致的 Skeleton。已有数据刷新失败时保留内容，并显示过期警告、最后成功时间和重试入口。
- 空态必须说明用途、前置条件和下一步，并提供唯一主操作。
- Profile 发布成功使用“已发布，尚未影响任何 Portal”；Activation 成功显示旧/新 revision、变化摘要和“刷新并查看”。
- Activation 显示“校验输入 → 生成快照 → Edge 就绪检查 → CAS 激活”阶段。失败必须指出阶段、原因和可重试性。
- 从未授予的操作不渲染；有权限但因对象状态不可执行的操作禁用并解释原因。
- 正常动效仅用于 Popover、Drawer、折叠和阶段进度，时长 120–180ms；数据刷新、页面切换和结果展示不使用装饰性动画。

## 8. 验收清单

- 375px、768px、1199px、1200px 和宽屏容器。
- 200% 缩放、RTL、47 字符菜单名、全部键盘操作、读屏与 reduced-motion。
- Arco/MUI 的 ESC、外部点击、焦点恢复、碰撞翻转和 Shadow DOM 行为一致。
- 顶部“更多”、标准侧栏多开分组、活动路径和权限裁剪在两种布局下结果一致。
- 未保存表单遇到内部导航或刷新提示时不会被自动丢弃。
