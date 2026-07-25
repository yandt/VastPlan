# React Runtime Engine

`cn.vastplan.foundation.frontend.runtime.engine.react` 是当前唯一正式 Frontend Runtime 制品。它由 Platform Profile 固定，普通应用插件不能选择或替换。

浏览器入口提供已签名的 React family/contract 兼容描述，用于约束 Renderer、Shell、Workbench 和模块图；当前 Browser Root、Hydration 与 React 组件树仍由 React Portal Kernel 持有，不把这份描述伪装成可独立替换的完整 Browser Engine。

同一制品的 `entry.frontendServer` 是真实的 Server Runtime Provider：它使用 `react-dom/server` 提供 SSR 启动壳，并由 Node Portal Kernel 按 Server Module Graph 物化到隔离 Worker Generation。该服务端生命周期、内容摘要和故障边界是本制品继续独立存在的依据。未来只有引入第二个 Browser Engine 时，才把 Root/Hydration/Generation 的浏览器桥接完整移入同一 Engine 契约。

该插件不是 UI 设计系统。Arco/MUI 仍由 `ui.render.adapter` 管理；功能页面继续通过 Workbench 声明。
