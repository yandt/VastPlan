# Unified Render Adapter

`cn.vastplan.foundation.frontend.render.adapter` 是 Portal 唯一的 `ui.render.adapter` 基础插件。

当前产品只选择受信任的 Ant Design Renderer。插件自身不保存 Renderer 插件 ID 或精确版本；可信宿主从已验证 Manifest Contribution Index 读取 `frontend.rendererModules`，再与 Platform Profile 的默认值、允许范围和主题策略组成当前 Generation 的精确 Catalog。未来新增 Renderer 只需下载安装其签名插件并由 Profile 选择，功能插件始终只依赖 `@vastplan/ui-primitives`。

切换 Renderer 会保存用户偏好、验证目标目录并重新装配 Portal Generation，随后刷新页面。它不会在同一 React 树或 DOM 中混用两个 UI 框架。
