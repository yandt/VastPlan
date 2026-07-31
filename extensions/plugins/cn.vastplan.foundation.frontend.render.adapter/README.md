# Unified Render Adapter

`cn.vastplan.foundation.frontend.render.adapter` 是 Portal 唯一的 `ui.render.adapter` 基础插件。

当前 Catalog 只包含受信任的 Ant Design Renderer。Platform Profile 仍通过通用字段声明默认 Renderer、允许范围、用户是否可选择及主题模板；未来新增 Renderer 只需提供独立签名模块并加入 Catalog，功能插件始终只依赖 `@vastplan/ui-primitives`。

切换 Renderer 会保存用户偏好、验证目标目录并重新装配 Portal Generation，随后刷新页面。它不会在同一 React 树或 DOM 中混用两个 UI 框架。
