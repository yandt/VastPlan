# Unified Render Adapter

`cn.vastplan.foundation.frontend.render.adapter` 是 Portal 唯一的 `ui.render.adapter` 基础插件。

Arco 与 MUI 是该插件的受信任内部 Renderer。Platform Profile 只声明默认 Renderer、允许范围、用户是否可选择，以及每个 Renderer 的主题模板；功能插件始终只依赖 `@vastplan/ui-primitives`。

切换 Renderer 会保存用户偏好、验证目标目录并重新装配 Portal Generation，随后刷新页面。它不会在同一 React 树或 DOM 中混用两个 UI 框架。
