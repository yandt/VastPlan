# Arco Design System Plugin

`com.vastplan.foundation.frontend.design-system.arco` 是 Portal 的首个第一方设计系统插件。它以 Arco Design 实现 `@vastplan/portal-ui` 的 `1.x` 契约，提供 PortalShell、布局、菜单/面包屑/标签/命令面板、弹窗/抽屉、动态表单、表格/描述/分页、状态反馈、语义图标与主题 token。

它不是业务页面，也不是门户内核的一部分。每个 Portal 发布版本只能选择一个 `ui.design-system`；可在另一个 Portal 中选择未来的 MUI 等适配器，但不能在同一 Portal 中混用。

功能插件只能从 `@vastplan/portal-ui` 获取语义化组件与 hooks，不能直接导入 Arco、修改全局 CSS 或管理顶级弹窗栈。Portal 内核在执行远程模块前验证该插件为已签名第一方制品、`uiContract` 兼容且基础 UI 能力完整。Arco 的 Overlay holder 位于当前 Portal Shadow DOM 内，不使用全局 `document.body` 服务。

动态表单在插件内部使用 RJSF 6 + AJV 8，接收 JSON Schema Draft 7 与可选 `uiSchema`。插件提供 Arco 文本、多行、数字、布尔、枚举/多选、日期、对象、数组、组合选择、增删排序、错误摘要和自定义凭证引用 widget；默认值、组合、条件与同步校验均遵循标准 JSON Schema。服务端校验通过可取消的异步 validator 注入。

凭证字段必须同时声明 `format: vastplan-credential-ref` 和 `writeOnly: true`，值只能是 `credential://...` 引用；`ui:widget: secretRef` 只控制 Arco 呈现，不是后端放行依据。表单禁止远程 `$ref`，公共 Portal SDK 不暴露 RJSF 或 Arco 类型。

```bash
pnpm --filter @vastplan/design-system-arco typecheck
pnpm --filter @vastplan/design-system-arco test
```

详见《[前端门户内核](../../../docs/dev/architecture/前端门户内核.md)》、[ADR-0052](../../../docs/dev/decisions/ADR-0052-前端门户内核与多UI设计系统插件.md)、[ADR-0064](../../../docs/dev/decisions/ADR-0064-Portal语义组件契约与动态表单运行时.md) 与 [ADR-0065](../../../docs/dev/decisions/ADR-0065-通用JSON-Schema表单与Arco主题适配.md)。
