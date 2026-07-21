# React Runtime Engine

`cn.vastplan.foundation.frontend.runtime.engine.react` 是当前唯一正式 Frontend Runtime Engine。它由 Platform Profile 固定，普通应用插件不能选择或替换。

当前 1.0 贡献浏览器 CSR、Portal Generation、按需模块和 i18n 能力。Node Portal Kernel 的 SSR Worker 完成后再以同一插件版本化 `serverEntry`，不能在清单中提前宣告未实现能力。

该插件不是 UI 设计系统。Arco/MUI 仍由 `ui.render.adapter` 管理；功能页面继续通过 Workbench 声明。
