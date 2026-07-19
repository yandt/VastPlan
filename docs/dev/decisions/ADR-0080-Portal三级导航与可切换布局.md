# ADR-0080 Portal 三级导航与可切换布局

- 状态：已采纳
- 日期：2026-07-19
- 修订：ADR-0074 中 `zone → group → page` 的两级导航模型，以及“标准布局只显示二级菜单”的补充约束
- 关联：[ADR-0052](ADR-0052-前端门户内核与多UI设计系统插件.md)、[ADR-0064](ADR-0064-Portal语义组件契约与动态表单运行时.md)、[ADR-0074](ADR-0074-Portal组合Slot与纯布局插件分层.md)

## 背景

Portal 已把设计系统、Shell 组合与 Shell 布局拆成三个独立基础插件，但原导航只允许 `zone → group → page`。随着平台插件增多，同一根功能区需要稳定的子分组；同时需要验证“顶部导航”和“侧栏导航”能够在不改变功能插件的前提下在线切换。若让两个布局分别解释任意递归菜单，导航深度、活动路径、权限裁剪和键盘行为会发生漂移。

## 决策

1. UI Contract 直接升级到 `2.0.0`，开发阶段不维护 1.x 布局兼容层。Manifest、Profile 和基础插件统一声明 2.x 范围。
2. Shell 组合输出有界导航树：`zone → root group → child group? → page`。`zone` 不计入可见深度；页面必须为叶子。根组可直接包含页面或子组，子组只能包含页面。
3. 分组 ID 在一份 Profile 内全局唯一。Go Schema、可信 Catalog、Composer 与浏览器组合器都校验父引用、跨 zone、循环、最大深度、重复 ID 和活动页面路径。
4. 组合层输出唯一 `activeNavigationPath = {zone, rootGroupID, childGroupID?, pageID}`。布局插件不得各自反推祖先。
5. 新增第一方 `com.vastplan.foundation.frontend.layout.top-navigation`。顶栏只显示根组；点击或键盘打开一个分组式 Mega Popover，承载根直属页面、子组和三级页面，不使用嵌套 Popover 或级联菜单。
6. 顶栏将 `primary` 与 `secondary` 放在中部并以视觉分隔区分，`settings` 固定在右侧。空间不足时尾部根组进入“更多”，活动根组优先保留。
7. `layout.standard` 保留 64px 图标轨与 240px 常驻面板；根直属页面优先显示，子组使用可多开的折叠章节。活动子组自动展开，展开状态只保存在浏览器会话。
8. UI Contract 2.0 增加框架无关的受控 `Popover` 和 Shell/Overlay 语义 token。Arco/MUI 负责定位、碰撞、ESC、外部点击、焦点恢复、Shadow DOM Portal 与 z-index；布局只拥有菜单结构。
9. 响应式按 Shell 容器宽度：`≥1200px` 完整桌面，`768–1199px` 收窄并使用“更多”，`<768px` 统一使用全高 Drawer。
10. 顶部导航采用 disclosure 语义；页面使用普通链接与 `aria-current`。标准图标轨实现 Up/Down/Home/End、ArrowRight 进入面板、ArrowLeft 返回。触控目标不小于 44×44px。

## 备选方案

- 把顶部导航做成 `layout.standard` 的配置模式：会让一个插件同时维护两套大结构，无法独立制品化和验证动态替换，拒绝。
- 使用无限递归菜单：服务端无法给出有界验证，多语言 SDK 也难以保持一致，拒绝。
- 顶部使用级联菜单：三级导航依赖 hover、横向空间和嵌套 Overlay，不适合触控与键盘，拒绝。
- 功能插件决定菜单位于顶部或侧栏：破坏布局插件边界，拒绝。

## 影响

- 正面：功能插件只声明语义树一次，两个布局、Arco/MUI 和未来框架共享同一活动路径与权限结果。
- 正面：顶部布局可作为独立签名制品进入 Platform Profile，并通过 PortalActivation 在线切换。
- 代价：UI Contract、Profile Schema、Go/TypeScript 类型和全部第一方 UI 插件必须一起升级到 2.0.0。
- 代价：需要新增真实浏览器测试覆盖容器宽度、溢出、焦点、RTL、200% 缩放和移动 Drawer。

