# MUI Design System Plugin

`cn.vastplan.foundation.frontend.render.adapter.mui` 是 Portal 的第二个第一方设计系统适配器。它以 Material UI 实现与 Arco 插件相同的 `@vastplan/ui-primitives` 1.x 语义契约，用于证明功能插件不依赖具体 UI 框架。

该插件提供完整的布局、导航、弹层、JSON Schema 表单、数据展示、反馈和主题能力。每个 Portal revision 仍只能由 Platform Profile 固定一个 `ui.render.adapter`；MUI 与 Arco 可用于不同 Portal，但不会在同一 Portal 中混用。

当前 MUI 适配器是契约基线实现。动态表单复用 RJSF + AJV 的标准 JSON Schema 语义，组件外观和高级 widget 可在不改变公共契约的前提下继续完善。

```bash
pnpm --filter @vastplan/ui-render-adapter-mui typecheck
pnpm --filter @vastplan/ui-render-adapter-mui test
```

详见《[前端门户内核](../../../docs/dev/architecture/前端门户内核.md)》和 [ADR-0052](../../../docs/dev/decisions/ADR-0052-前端门户内核与多UI设计系统插件.md)。
