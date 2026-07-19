# ADR-0074：Portal 组合 Slot 与纯布局插件分层

- 状态：已接受
- 日期：2026-07-19

## 背景

旧 Portal Shell 直接拼接 Page/Menu，设计系统同时承担组件、主题和全局布局，功能插件通过 `addRoute/addMenu` 自己生成页面外壳。这样 LOGO、菜单方向、系统设置位置和 page-header/page-body 样式无法作为独立平台能力替换；如果布局插件自行定义 Slot，换布局又会破坏功能插件契约。

## 决策

Portal Platform Profile 必须分别固定三个相互独立、已签名的第一方插件：

1. `ui.render.adapter`：提供框架无关语义组件、主题和 Overlay/Form/Data 实现，不拥有 Portal 信息架构。
2. `ui.structure.composition`：拥有稳定页面模型、导航语义区、Slot 目录、作用域、排序和冲突规则；首个实现为 `cn.vastplan.foundation.frontend.structure.composition.standard`。
3. `ui.structure.layout`：只消费标准化组合模型并决定视觉排布；首个实现为 `cn.vastplan.foundation.frontend.structure.layout.standard`。

标准 Slot 由组合契约统一版本化，包括：

- `shell.header.start|center|end`
- `shell.navigation.before|after`
- `page.header.start|center|end`
- `page.body.before|main|after`
- `page.aside`
- `shell.footer`

功能插件改用 `addPage`，只能声明页面 ID/路径/标题、`primary|settings|secondary` 导航语义区，并向现有 Slot 填充组件。它不能创建全局 Slot、决定菜单位于顶部还是侧栏、放置 LOGO、设置页面宽度或绘制 Page Shell。每个页面必须至少填充 `page.body.main`。

布局私有配置由 Platform Profile 的 `layout.config` 提供；Application Composition 不能选择或覆盖设计系统、组合插件和布局插件。

## 结果

- Slot 拓扑在更换 Arco/MUI 或顶部/侧栏布局时保持一致。
- LOGO 位置、系统设置区、page-header/page-body 样式可独立演进。
- 功能插件只保留功能视图与语义声明，Portal Kernel 只保留可信装配和路由选择。
- 三个基础插件任一缺失、来自应用输入、契约不兼容或出现第二贡献时均拒绝发布和浏览器装配。

## 补充约束：空区域折叠

标准布局以“是否存在实际内容”决定区域是否渲染，实际内容包括 Slot 贡献、导航项和布局按配置放置的 LOGO、页面标题等内建内容。Shell Header、侧栏、顶部设置区、Page Aside 与 Footer 完全为空时不创建 DOM 和占位尺寸；Page Header 因包含页面标题与可选描述，不因三个扩展 Slot 为空而折叠。该判断属于 `ui.structure.layout` 的视觉职责，组合插件不引入 `visible` 字段。

## 2026-07-19 补充决策：图标主轨与常驻二级导航

原 `shell.navigation.before|after` 不能准确表达受管理导航的三段所有权，废止并替换为 `shell.navigation.start|center|end`。全局 `shell.*` 贡献与活动页面 `page.*` 贡献必须分开归并：全局贡献只允许来自 Platform Profile 锁定的插件，Application Composition 插件即使已签名也不能借活动页面改写品牌、主导航或账户区域。

导航固定为有界的 `zone → group → page`。页面只声明 `groupID`；分组的 `id / label / zone / icon / order` 由 `ui.structure.composition` 通过 Platform Profile 的 `composition.config.navigationGroups` 治理。未显式分组的页面进入所属 zone 的内建同名组；引用未知分组或跨 zone 分组必须拒绝组合。布局插件只能消费标准化后的非空分组，不能自行猜测业务分组或开放任意二级菜单 Slot。

标准桌面布局采用稳定双栏方案：默认 64px 图标主轨，右侧常驻 240px 二级导航栏。点击主轨图标只切换二级导航上下文；选择二级页面才触发路由跳转。二级导航是正常布局区域，不使用 Popover/Flyout，也不覆盖正文。主轨 `center`、主轨 `end` 和二级导航各自拥有有界纵向滚动，Page Header 位于 Page Body 滚动容器之外。平板可缩窄二级栏，手机隐藏桌面双栏并改用全高 Drawer；账户命令仍可使用 Overlay，因为它不是页面层级导航。
