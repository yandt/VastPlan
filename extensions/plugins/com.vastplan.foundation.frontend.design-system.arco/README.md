# Arco Design System Plugin

`com.vastplan.foundation.frontend.design-system.arco` 是 Portal 的首个第一方设计系统插件。它以 Arco Design 实现 `@vastplan/portal-ui` 的 `1.x` 契约，提供 PortalShell、布局、菜单/面包屑/标签/命令面板、弹窗/抽屉、动态表单、表格/描述/分页、状态反馈、语义图标与主题 token。

它不是业务页面，也不是门户内核的一部分。每个 Portal 发布版本只能选择一个 `ui.design-system`；可在另一个 Portal 中选择未来的 MUI 等适配器，但不能在同一 Portal 中混用。

功能插件只能从 `@vastplan/portal-ui` 获取语义化组件与 hooks，不能直接导入 Arco、修改全局 CSS 或管理顶级弹窗栈。Portal 内核在执行远程模块前验证该插件为已签名第一方制品、`uiContract` 兼容且基础 UI 能力完整。Arco 的 Overlay holder 位于当前 Portal Shadow DOM 内，不使用全局 `document.body` 服务。

动态表单完整支持 `text`、`textarea`、`number`、`boolean`、`select`、`multiSelect`、`date`、`object`、`array` 和 `secretRef`。默认值、条件显示与同步校验来自共享 `@vastplan/ui-contract` 运行时；服务端校验通过可取消的异步 validator 注入。`secretRef` 只表示凭证引用，不得传入凭证明文。

```bash
pnpm --filter @vastplan/design-system-arco typecheck
pnpm --filter @vastplan/design-system-arco test
```

详见《[前端门户内核](../../../docs/dev/architecture/前端门户内核.md)》、[ADR-0052](../../../docs/dev/decisions/ADR-0052-前端门户内核与多UI设计系统插件.md) 与 [ADR-0064](../../../docs/dev/decisions/ADR-0064-Portal语义组件契约与动态表单运行时.md)。
