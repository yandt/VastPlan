# ADR-0086 单 Shell 插件与可切换布局模板

- 状态：已采纳（实施中）
- 日期：2026-07-19
- 取代的范围：[ADR-0074](ADR-0074-Portal组合Slot与纯布局插件分层.md) 中“组合与布局必须由不同插件提供”的约束，以及 [ADR-0080](ADR-0080-Portal三级导航与可切换布局.md) 中“顶部导航必须是独立制品”的约束
- 关联：[ADR-0064](ADR-0064-Portal语义组件契约与动态表单运行时.md)、[ADR-0078](ADR-0078-Frontend事务式热替换与插件生命周期.md)、[ADR-0083](ADR-0083-前端UI分层术语与插件命名空间.md)、[ADR-0085](ADR-0085-渲染适配器主题模板契约.md)

## 背景

当前 Portal 将 `ui.structure.composition` 与 `ui.structure.layout` 分别装配为独立的第一方插件。这个拆分成功保护了功能插件所依赖的 Slot、导航树和活动路径：更换侧栏或顶部布局不会令功能插件重新解释业务结构。

但是“标准侧栏”和“顶部导航”现在是两个独立布局制品。用户切换布局需要改变 Platform Profile 的基础插件选择并生成新的 Activation；同一个 Shell 的语义骨架也被拆到第三个制品中。对于“管理员规定可选范围、用户按偏好即时切换布局”的需求，这带来不必要的装配、发布和状态恢复成本。

不能以删除 Slot 层来换取简化。这样会让功能插件知道当前是顶部还是侧栏，并重新获得布局控制权；将来第三种布局会再次改变功能插件契约。

## 决策

### 1. 一个基础 Shell 插件，两个固定职责

Portal Profile 不再分别选择 `structureComposition` 与 `structureLayout`，改为选择唯一的 `shell` 基础插件。首个实现为 `cn.vastplan.foundation.frontend.structure.shell`。

它对外只贡献 `ui.structure.shell`，在一个已验证的前端模块内同时拥有：

1. 固定的语义骨架：页面/导航归并、Slot 目录、排序、作用域、活动导航路径和空区域判断；
2. 可发现的布局模板目录：`standard`、`top-navigation` 及未来模板。

这不是让布局模板定义自己的 Slot。所有模板都消费同一份 `ShellCompositionModel`，功能插件继续只能通过既有 `addPage` 与 `addShellContribution` 填充稳定语义 Slot。模板可以重新排布区域、选择抽屉或 Popover、调整响应式和滚动容器，但不得重命名、删除或把已填充的 Slot 静默丢弃。

### 2. Shell 模板为受治理目录，不是任意 CSS 配置

`UIShellAdapter` 必须声明稳定 `id: "ui.structure.shell"` 与 UI contract 范围、`templates[]`（稳定 `id`、可本地化 `label`、受限展示元数据）、`defaultTemplate`、`compose(input)` 及接收归并模型和模板选择的 `Shell`。

模板是同一可信模块中的代码实现，不能由 Profile 传入 CSS、DOM 片段、组件名、URL 或任意 JSON 规则。渲染适配器仍是唯一拥有框架主题、token、DOM/Overlay 细节的基础插件；主题模板继续由 `ui.render.adapter` 管理，不与 Shell 模板混合。

### 3. 平台选择范围，用户选择当前模板

Profile 的 `shell.config` 采用如下受限结构：

```json
{
  "defaultTemplate": "standard",
  "allowedTemplates": ["standard", "top-navigation"],
  "userSelectable": true,
  "templateOptions": {
    "standard": {},
    "top-navigation": {}
  }
}
```

- 平台管理员只能选择 Shell 插件已声明的模板；空、重复、未知或未授权模板在发布与浏览器装配时 fail-closed。
- Profile 决定默认值和允许范围，不能强制用户选择某一套框架主题或修改语义 Slot。
- 浏览器只在 `userSelectable=true` 且偏好位于 `allowedTemplates` 时读取用户偏好；否则使用 Profile 默认值。
- 用户偏好由 Portal 宿主保存为以 `tenant/portal/shell-plugin` 隔离的本地轻量数据，不进入 Application Composition、Activation 或服务端审计输入。下一次刷新即可恢复；切换时只更新 Shell React 状态，不创建新的 Portal Generation。
- 管理员撤销某模板时，浏览器在下一次加载或 Profile Generation 变更时回退到新的默认模板，并覆盖无效本地偏好。

### 4. 固定 Slot 兼容性与状态边界

所有模板必须支持标准 Slot 集：

- `shell.header.start|center|end`
- `shell.navigation.start|center|end`
- `page.header.start|center|end`
- `page.body.before|main|after`
- `page.aside`
- `shell.footer`

“区域为空则不渲染”的规则保留，但只允许折叠没有贡献且没有内建内容的区域。模板不得因为视觉不需要某区域而隐藏其中的已注册贡献；若需要紧凑表示，必须为该 Slot 定义明确且可访问的承载位置，例如账户区溢出菜单。

模板切换不能重取模块、重新注册插件、改变路由、导航树、权限裁剪、Workbench 偏好或当前 URL。Shell 需要把同一已组合内容作为稳定子树传给模板，尽量避免页面和 Workbench 被无意义卸载；模板自身短暂状态（例如导航抽屉开启）允许重置。表单脏状态的导航保护继续由 Workbench/页面层负责，不借布局切换绕过。

### 5. 开发阶段一次性迁移

项目尚未承诺对外兼容，直接升级为新的 Shell 契约：

- Profile、Catalog、Portal Runtime、Portal API、Schema 和测试夹具只接受 `shell`，不保留 `structureComposition` / `structureLayout` 的双字段；
- Manifest 只接受 `shells` 贡献，旧 `structureCompositions`、`structureLayouts` 及三个旧基础插件目录在迁移完成后删除；
- UI Contract 升至新的 major 版本，所有首方 UI 基础插件、示例 Profile 和开发工具在同一变更中切换。

## 备选方案

### 维持两个插件，允许它们引用同一制品

可少量减少交付制品，但 Profile 仍要维护两个可不兼容的选择，运行时仍需验证两个贡献，用户切换仍像修改平台基础设施。拒绝。

### 以第三个“布局选择器”插件协调现有三个插件

会继续保留三份装配关系和双重版本兼容，选择器反而成为隐式第四个基础单例。拒绝。

### 合并插件并删除 Slot 语义层

功能插件会重新耦合到模板，布局切换不再安全，也无法做未来布局扩展。拒绝。

### 每种模板一个独立 Shell 插件

适用于不同信息架构或不同产品壳，但不适合相同语义结构下的用户偏好切换。后续若出现不兼容信息架构，可另选一个 Shell 插件；同一 Shell 内的模板仍只表示视觉与交互排布。

## 影响

- 正面：顶部与侧栏变为一个受治理 Shell 内的即时可切换模板，用户不再因个人显示偏好触发 Activation。
- 正面：Slot 契约、导航归并和权限边界仍只有一份实现，功能插件不感知布局样式。
- 正面：模板目录与主题目录职责分开，未来增加 UI 框架或主题不会放大 Shell 复杂度。
- 代价：这是 UI 契约、Profile Schema、Portal Runtime、Catalog 和三份现有插件的一次破坏性迁移，必须以端到端测试完成，不能只替换前端展示代码。
