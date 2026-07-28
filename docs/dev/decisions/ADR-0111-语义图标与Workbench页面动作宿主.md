# ADR-0111 语义图标与 Workbench 页面动作宿主

- 状态：已采纳
- 日期：2026-07-22

## 背景

页面级“新增、导入、发布”等动作此前与集合工具栏混在一起，导致布局职责、集合职责和业务职责交叉。Arco Renderer 使用框架图标，MUI Renderer 使用 Unicode 字符，同一动作在切换 Renderer 后形状、尺寸和可访问性也不一致。若让功能插件直接填充 `page.header.end`，又会重新获得任意组件和布局控制能力。

## 决策

1. UI Contract 提供稳定的语义图标词表；功能插件只引用图标名，不引用 React 组件、SVG 内容或 Arco/MUI 图标。
2. `@vastplan/ui-primitives` 提供完整的 VastPlan 自有 SVG 目录作为 `canonical` 基线；每个 Renderer 还可提供 `renderer-native` 目录。两者共享同一语义名称、尺寸和可访问性契约，原生目录缺项时必须回退 `canonical`。
3. Platform Profile 通过每个 Renderer 的 `rendererOptions.iconTheme` 选择 `canonical` 或 `renderer-native`。Renderer 必须声明 `iconThemes/defaultIconTheme`，Portal Kernel 在加载阶段拒绝未声明的主题。只有被选中的 Renderer 及其按需图标模块进入浏览器，不会同时加载 Arco 和 Material 图标库。
4. `page.primary/page.secondary` 动作必须声明语义图标。Workbench 通过一个页面动作控制器共享选择、可见性和执行状态；Portal Kernel 将 Workbench 的动作宿主挂入标准 `page.header.end`。
5. 桌面 Page Header 使用纯图标、Tooltip、`aria-label` 和最小 44px 点击区；最多直接显示 4 个，更多动作进入一个可访问的溢出菜单。
6. `collection.bulk` 留在集合范围内，使用“选择动作 + 明确执行”两步交互；新增、导入、发布等页面级动作不得回到 Table 工具栏。
7. Slot 层继续保留为 Shell 的稳定结构契约，但功能插件不直接管理这个 Slot；可信 Portal Host 负责把 Workbench 语义动作编译成 Slot 贡献。

## 2026-07-28 修订：选择面只服务批量动作

Table 行选择与 Card 选择统一由权限投影后的 `collection.bulk` 动作驱动。没有有效批量动作时 Workbench 强制关闭选择面，不因历史 `selection` 配置、行操作或 Page Header 动作显示单选框/复选框；存在批量动作时显式 `single` 仍可保留，未声明或声明其他模式统一使用 `multiple`。功能插件不能直接控制 Renderer 的选择列可见性。

## 备选方案

- **全部固定为自有 SVG**：跨 Renderer 最一致，但不能利用当前设计系统的完整图标生态，也无法提供用户选择。
- **各 Renderer 只使用自己的图标库**：能获得框架原生外观，但缺少稳定基线和缺项回退，跨 Renderer 行为容易漂移。
- **功能插件直接提供 SVG/组件**：灵活，但破坏 Workbench 治理、CSP 边界、主题一致性和跨端可移植性。
- **把所有动作留在 Table 工具栏**：实现简单，但页面级命令和集合级命令无法区分，卡片、树和详情页也不能复用。
- **移除 Slot 层**：会把 Workbench 与具体 Layout 直接耦合，使 Shell Library 切换失去稳定结构协议。

## 影响

- `canonical` 下 Arco、MUI 的图形完全一致；`renderer-native` 下允许视觉遵循当前框架，但语义、尺寸、Tooltip、焦点和无障碍规则保持一致。
- Material 原生主题会增加选中 MUI Renderer 的图标模块体积；按子路径导入和构建体积门禁限制这项成本。功能插件制品不增加 UI 框架依赖。
- 页面动作与集合正文通过小型外部控制器通信，能够保持现有 Workbench 表单、Overlay、选择和刷新状态。
- 新增语义图标需要先扩展受治理词表和测试，不能由业务插件临时注入。
- 当前页面级动作需要补充语义图标；这是开发期契约收紧，不提供旧数据兼容层。

## 后续修订

- 2026-07-28：canonical 的“VastPlan 自有 SVG”来源决策由 [ADR-0164](ADR-0164-MIT图标目录与分片按需加载.md) 修订为精确锁定、MIT 许可的 Ant Design 图标目录。稳定语义词表、跨 Renderer 几何一致、原生主题回退和功能插件禁止传入 SVG 的边界保持不变。
- 2026-07-28：页面动作的选择态控制器与 `page.primary/page.secondary` placement 由 [ADR-0166](ADR-0166-独立页面动作宿主与固定帮助入口.md) 取代。语义图标、统一 Tooltip、Slot 隔离和溢出上限继续有效。
