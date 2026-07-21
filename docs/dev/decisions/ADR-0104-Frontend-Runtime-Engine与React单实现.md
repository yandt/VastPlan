# ADR-0104 Frontend Runtime Engine 与 React 单实现

- 状态：已采纳，已完成
- 日期：2026-07-21
- 关联：[ADR-0083](ADR-0083-前端UI分层术语与插件命名空间.md)、[ADR-0087](ADR-0087-统一Render-Adapter与可切换Renderer.md)、[ADR-0103](ADR-0103-Node-Portal-Kernel渐进替代Go-Edge.md)

## 背景

React/Vue 是运行引擎，Arco/MUI 是组件与设计系统实现。现有 Frontend 把 React Root、组件类型和共享依赖直接固化在 Portal Kernel，同时把 Arco/MUI 放在统一 Render Adapter 内部。若为了宣称多引擎立即维护 React/Vue 两套 Shell、Workbench、表单、SSR、HMR 和测试矩阵，成本会接近两至三倍，且用户几乎得不到直接业务价值。

## 决策

1. 增加第四个 Frontend 基础单例扩展点 `ui.runtime.engine`。Platform Profile 必须精确选择 Engine、Render Adapter、Shell 和 Workbench，Application Composition 不能替换它们。
2. 当前唯一生产实现为 `cn.vastplan.foundation.frontend.runtime.engine.react`。它提供 browser/server entry、Root、Hydration、Generation 生命周期和 React 单例依赖。
3. 契约、Resolver、RuntimeSpec 和模块图必须使用 `engineFamily`/`engineContract`，不得硬编码 React。当前不创建 Vue 占位插件、不在 UI 展示 Vue，也不承诺 Vue 兼容。
4. Renderer 模块以 `(engineFamily, rendererID)` 为复合身份。当前允许 `react/arco`、`react/mui`；MUI 不可配置到未来 Vue Engine。
5. 普通功能插件只使用框架无关的 `ui-contract` 与 `workbench-sdk`。React、Vue、JSX/TSX、组件库、DOM、全局 CSS 和裸页面注册由构建门禁拒绝。
6. 复杂视觉能力以受治理 Workbench Pattern Provider 扩展。只有 Foundation/经批准的可信 Provider 可以使用 Engine Bridge，不能替换 Shell、Router 或权限边界。
7. Engine 变化属于 Host Epoch，需要完整候选验证和受控刷新；不得在同一 DOM/页面树混合两个 Engine。

## 备选方案

- **立即实现 React 与 Vue**：兼容面最大，但测试矩阵和长期维护成本没有当前需求支撑；拒绝当前实施。
- **继续硬编码 React**：短期最少改动，但未来新增 Engine 会再次改 Profile、Catalog、RuntimeSpec 和插件 SDK；拒绝。
- **把 Vue 页面嵌入 React Portal**：形成双运行时状态和样式边界，又不是真正可替换 Engine；拒绝。

## 影响

- Frontend 基础角色从三个增为四个：Runtime Engine、Render Adapter、Shell、Workbench。
- React 仍是唯一实际运行时，因此当前页面和插件体验不因本 ADR 自动改变。
- Vue 的启动信号是明确客户/生态需求以及公共 SDK 已无 React 类型，而不是仅为技术对称。

## 实施记录（2026-07-22）

- 已建立框架无关的 `frontend-engine-contract`，覆盖 Engine 身份、能力、Browser Root、Server Runtime、Generation 与生命周期校验。
- Platform Profile、Catalog、Resolver、RuntimeSpec 和模块图均显式携带 `engineFamily`/`engineContract`，并在装配时校验 Runtime Engine、Render Adapter、Shell 与 Workbench 的 Engine family 一致性。
- `cn.vastplan.foundation.frontend.runtime.engine.react` 已作为唯一生产实现提供 CSR、SSR、hydration、Generation、按需模块和 i18n 能力；Arco/MUI 继续作为 React family 下的 Renderer，而不是独立 Engine。
- 功能插件继续只依赖框架无关的 UI Contract 与 Workbench SDK；生产构建和架构守护拒绝功能插件直接引入 React、Vue、Arco、MUI、DOM 或裸页面注册。
- 当前不提供 Vue 占位实现。增加新 Engine 时必须以独立基础插件实现同一契约并通过完整构建、SSR、热替换、安全与双端测试矩阵。
