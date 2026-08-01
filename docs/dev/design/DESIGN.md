# VastPlan Portal 设计系统

> 状态：设计基线 v4｜最后更新：2026-07-31
>
> 本文是 Portal 跨布局、基于 Ant Design 当前实现的视觉与交互单一真相源。组件职责和安全边界见《[前端门户内核](../architecture/前端门户内核.md)》，Renderer 收敛见 [ADR-0179](../decisions/ADR-0179-Ant-Design单实现与Renderer协议保留.md)。

## 1. 设计原则

1. Portal 是密集但可扫描的企业管理工作区，不使用营销式 Hero、卡片仪表盘、装饰渐变或无意义图标。
2. 每个区域只有一个任务。列表负责选择，详情负责理解，编辑器负责修改，Activation 流程负责上线。
3. 当前线上 Activation 是 Portal 详情的第一视觉锚点；草稿和 Published 输入不能伪装成线上状态。
4. 颜色不单独承载含义；活动、失败、警告和禁用同时使用形状、位置、文字或图标。
5. 区域宿主管理自己的溢出。结构化菜单可进入“更多”；任意插件内容只能按区域策略滚动、换行、截断或折叠。

## 2. 基础 Token

UI Contract 8.3 暴露语义 token、账户外观契约与 `ComponentSize`，适配器映射到具体框架。布局插件不得读取 Ant Design 私有 token。列表、卡片、表单和操作区的一致性交给《[UI 工作台组合框架](../architecture/UI工作台组合框架.md)》；布局只决定它们所在区域的视觉位置。

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

### 2.1 组件 Recipe 治理

- 本文定义视觉意图和验收基线；`extensions/sdk/ts/ui-primitives/src/visual-recipes.ts` 保存必须跨 Renderer 一致的可执行数值。组件架构文档只描述职责和行为，不重复保存像素值。
- 基础控件统一使用 `size=sm/md/lg`。Table 单独保留 `density=compact/standard/comfortable`，只调整表格行高和单元格留白，不向内部图标或表格外组件传播。功能插件不得导入 Ant Design 或写框架私有样式。
- 调整公共组件时，先更新本节的视觉基线和共享 recipe，再同步各 Renderer 映射、自动测试及浏览器截图验收；禁止在单个页面反复覆盖样式。
- 只有通用语义确实不足时才扩展 UI Contract。不得为了修正某个框架的默认边框、间距或图标而让业务插件感知框架差异。

| Size | 通用控件高度 | IconButton / 图标 | 菜单项 | 菜单表面 |
|---|---:|---:|---:|---:|
| `sm` | 24px | 18px / 12px | 28px | 最小宽 180px，内边距 3px，2px 圆角 |
| `md` | 32px | 32px / 16px | 36px | 最小宽 200px，内边距 4px，4px 圆角 |
| `lg` | 40px | 44px / 20px | 44px | 最小宽 220px，内边距 6px，6px 圆角 |

动作菜单每行固定为“图标 + 标签”，触发器使用图标 + Tooltip。危险动作保留语义危险色；普通动作不使用实心 primary 背景。Menu 的 `variant=action` 消除导航专用 inline-end 分隔线，并采用内容内在宽度（最小 112px、最大 280px）；菜单表面统一保留 4px 内边距，Popover 外层不再叠加内边距。超长标签在 216px 后省略且保留完整 title。`sm` 动作菜单使用 28px 行高，图标与标签间距固定为 6px，项目内边距为 inline-start 12px、inline-end 6px，且项目必须占满菜单容器；渲染器必须清除自身会叠加到该语义间距的默认图标/标题边距。页面级与记录级“更多”都复用 Workbench 的同一紧凑动作菜单。`variant=navigation` 保留导航语义；variant 与 size 互不替代。

## 3. 排版

- 自托管 `Noto Sans` 与 `Noto Sans SC`，按 Unicode Range 分包并使用 `font-display: swap`。
- 只预加载 Latin 与常用中文子集，其余按页面实际字形加载；字体失败时回退到平台无衬线字体，但不得阻塞 Portal 启动。
- 正文视觉字号不小于 16px；辅助信息可使用 14px，但必须满足 4.5:1 对比度。
- Page Title 桌面为 22px，移动端为 20px；长标题单行省略，tooltip 和可访问名称保留全文。

## 4. Shell 与布局

### 4.1 公共区域

- `shell.header.*`、`shell.navigation.*`、`page.header.*`、`page.body.*`、`page.aside` 和 `shell.footer` 的拓扑由组合插件统一管理。
- Shell Header、Aside、Footer 等没有内建内容且全部 Slot 为空时不创建 DOM 和占位；Page Header 因承担页面定位始终存在。
- Page Header 位于 Page Body 滚动容器之外。正文滚动时保持可见，不依赖多层 `position: sticky`。Page Body 使用设计系统 `surface` 语义色：浅色主题为白色，深色主题由当前 Renderer 的深色表面色接管。
- Page Body 支持页面级 `fluid / large / medium / small` 语义尺寸，唯一最大宽度分别为“不限制 / 1280px / 960px / 720px”。两种 Shell 必须统一居中整个正文 Slot 区域；移动端始终占满可用宽度。模板级 `contained` 是平台上限，和页面尺寸同时存在时取更窄值。插件不得传入任意像素或 CSS。
- 页面间距使用唯一 `portalPageRhythm`：Shell 从 Page Header 底边到 Workbench 根容器统一保留 16px `contentStart`；Workbench 根容器固定 `margin: 0; padding: 0`，并按 compact/standard/comfortable 使用 8/16/24px `sectionGap` 管理一级组件间距。一级组件不得用外部 margin 改写位置；FilterPanel 等可通过 Workbench 内部的 `flush=0` 或 `compact=8px` inset 管理自身内容，但 inset 不得反向补偿 Shell。Collection 顶部 FilterPanel 默认 `flush`；三个 Renderer 的 compact Form 必须隐藏根 Object Schema 标题并清除根外边距，嵌套对象标题不受影响，从而使第一行控件与页面起始节奏可预测。
- FilterPanel 使用 `inside-inline` 持久 Label：Label 与输入控件共同消费一个筛选单元格宽度，Label 按内容取宽但桌面最多占 40%、移动端最多占 45%，始终单行；超长文案省略并由 Tooltip 与可访问名称提供全文。输入区域必须 `flex: 1; min-width: 0`，输入后 Label 不消失。Ant Design 实现必须遵守该语义，功能插件不能配置像素宽度或注入框架样式。
- Page Header 右侧的页面功能动作使用 VastPlan 语义图标、Tooltip 和 `aria-label`，点击区至少 44px；桌面最多直接显示 4 个，超出后进入“更多”，不得在 Table 工具栏重复显示新增、导入或发布。
- 图标风格由 Renderer 的 `iconTheme` 统一决定：`canonical` 使用锁定的 MIT Ant Design 语义入口保持跨框架几何一致，`renderer-native` 使用当前 UI 框架原生图形并在缺项时回退。846 个原始目录名称不属于页面契约，只能由 Foundation 图标工具按 27 个分片延迟读取；单个页面和功能插件不得混指定图标来源。
- 已认证 Shell 只提供一个圆形账户头像入口；头像固定映射到统一组合模型中的 `account` 根分组。标准侧栏点击后在右侧常驻面板加载二级/三级菜单，顶部布局复用同一分组数据；头像本身不得硬编码功能项。Frontend Platform Profile 通过必填 `accountCenter` 选择个人中心实现，默认基础插件 `cn.vastplan.foundation.frontend.identity.account-center` 注册“用户信息”和“用户设置/外观”页面，且不能由 Application 删除或覆盖；后续账户功能继续走相同页面契约。`settings` 区域显示为“系统管理”，必须是最后一个一级导航项；企业用户、组织和角色管理属于该区域，不与个人中心合并。外观配置只保存在当前浏览器，并明确提示不会上传服务器。

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

### 4.5 会话前 Access 页面

- 登录页使用与当前 Frontend Platform Profile 相同的 Runtime、Renderer、Shell 和 Workbench，不另建基础组件或框架专用页面。
- `access` 模板不显示主菜单、Page Header、Page Body Slot 或功能插件区域。固定结构为 Access Header、单任务 Auth Panel 和 Access Footer。
- Auth Panel 桌面建议宽 400–440px，移动端占满安全边距；宽屏品牌区只能由受治理模板和内容寻址资产提供，不能成为营销式 Hero。
- 两种登录方式使用同一面板内的分段切换；超过三种时改为方法列表。切换时只允许复用 identifier，密码和验证码必须清空。
- 密码字段使用 `current-password`，验证码使用 `one-time-code`；错误放在面板内 `role=alert` 区域，账号不存在与凭证错误不得使用不同文案。
- 会话前语言和主题使用 Access Profile 引用的 Platform Profile 默认值。用户级偏好只能在 Session 建立后生效。

## 5. Overlay 与导航交互

- `Popover` 为受控语义组件，适配器拥有定位、碰撞翻转、外部点击、ESC、焦点恢复、Shadow DOM Portal 和 z-index。普通工作区 Popover 使用语义 overlay surface、1px 边框、6px 圆角、12px 内容内边距和 overlay 阴影；内部组合再管理自己的紧凑间距，不能把内容直接叠在页面上。
- 顶部导航采用 disclosure pattern：Enter、Space、ArrowDown 打开；Escape 关闭；Left/Right/Home/End 在根组间移动。
- 打开后优先聚焦当前页面链接，否则聚焦第一个链接。页面保持正常链接语义并使用 `aria-current="page"`。
- 标准图标轨支持 Up/Down/Home/End；ArrowRight 进入面板，ArrowLeft 返回原根组。
- 折叠子组按钮使用 `aria-expanded`。所有交互支持键盘-only、读屏、RTL、200% 缩放和 reduced-motion。

## 6. 管理工作区

- 平台管理中心分为 `Platform Profiles` 与 `Portals` 两个工作区。
- Table density 只改变表格自身行高与单元格留白。行操作按钮、图标和 action 菜单固定使用 `sm`；筛选表单固定使用自己的紧凑尺寸，工具栏、分页、卡片和页面间距均不受 Table density 影响。
- Table 列设置使用随最长列名变化、最大 280px 的紧凑 Popover：密度为最小按钮式单选，列顺序使用拖拽句柄且支持键盘移动，显隐使用眼睛/划线眼睛图标并固定右对齐；拖拽区最高 256px 并独立滚动，隐藏列统一使用 muted 灰色，不使用文字显隐按钮、上下箭头按钮或确认步骤。
- 集合工具栏左侧只承载批量操作与集合动作；刷新、列设置等展示控制统一归组在右侧，换行后仍保持右对齐。
- 拖拽句柄、目标反馈和排序结果由 Workbench 统一提供，业务插件不得自行选择拖拽库。未来首页卡片网格必须使用受治理断点、稳定卡片 ID 和用户布局偏好；普通管理资源页继续使用 Table/MasterDetail，不因引入 Dashboard Grid 改成卡片墙。
- 使用 master-detail 或详情路由，不采用资源卡片网格。
- Portal 详情顶部使用连续状态带展示当前 Activation、Profile、Application、Binding、健康状态与生效时间。
- Application、Profile、Binding 的 Published 状态只表示可被引用。Activation 才使用“准备中、激活中、当前生效、已取代、失败”。
- Activation 与回滚都使用独立全页流程：选择 → 校验/差异 → 确认 → 阶段进度 → 持久结果。
- 差异首先按布局、设计系统、插件、服务绑定、权限和路由展示语义摘要；原始 JSON 仅为可展开技术详情。

## 7. 状态与反馈

- 首次加载使用与最终结构一致的 Skeleton。已有数据刷新失败时保留内容，并显示过期警告、最后成功时间和重试入口。
- 空态必须说明用途、前置条件和下一步，并提供唯一主操作。
- Profile 发布成功使用“已发布，尚未影响任何 Portal”；Activation 成功显示旧/新 revision、变化摘要和“刷新并查看”。
- Activation 显示“校验输入 → 生成快照 → Portal Kernel 就绪检查 → CAS 激活”阶段。失败必须指出阶段、原因和可重试性。
- 从未授予的操作不渲染；有权限但因对象状态不可执行的操作禁用并解释原因。
- 正常动效仅用于 Popover、Drawer、折叠和阶段进度，时长 120–180ms；数据刷新、页面切换和结果展示不使用装饰性动画。

## 8. 验收清单

- 375px、768px、1199px、1200px 和宽屏容器。
- 200% 缩放、RTL、47 字符菜单名、全部键盘操作、读屏与 reduced-motion。
- Ant Design 的 ESC、外部点击、焦点恢复、碰撞翻转和 Shadow DOM 行为必须符合统一 Overlay 语义；未来 Renderer 接入同一门禁。
- 顶部“更多”、标准侧栏多开分组、活动路径和权限裁剪在两种布局下结果一致。
- 未保存表单遇到内部导航或刷新提示时不会被自动丢弃。
