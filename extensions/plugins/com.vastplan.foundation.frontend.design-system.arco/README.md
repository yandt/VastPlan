# Arco Design System Plugin

`com.vastplan.foundation.frontend.design-system.arco` 是 Portal 的首个第一方设计系统插件。它以 Arco Design 实现 `@vastplan/portal-ui` 的 `1.x` 契约，提供布局、菜单、弹窗、动态表单、数据反馈与主题能力。

它不是业务页面，也不是门户内核的一部分。每个 Portal 发布版本只能选择一个 `ui.design-system`；可在另一个 Portal 中选择未来的 MUI 等适配器，但不能在同一 Portal 中混用。

功能插件只能从 `@vastplan/portal-ui` 获取语义化组件与 hooks，不能直接导入 Arco、修改全局 CSS 或管理顶级弹窗栈。Portal 内核在执行远程模块前验证该插件为已签名第一方制品、`uiContract` 兼容且七项基础 UI 能力完整。

```bash
pnpm --filter @vastplan/design-system-arco typecheck
```

详见《[前端门户内核](../../../docs/dev/architecture/前端门户内核.md)》与 [ADR-0052](../../../docs/dev/decisions/ADR-0052-前端门户内核与多UI设计系统插件.md)。
