# MUI Design System Plugin

`cn.vastplan.foundation.frontend.render.adapter.mui` 是统一 Render Adapter 的内部 MUI Renderer 模块。它以 Material UI 实现与 Arco 相同的 `@vastplan/ui-primitives` 语义契约；Portal 只会在 Adapter 目录选中 `mui` 时下载它，不单独贡献 `ui.render.adapter`。

统一 Adapter 负责目录、Profile 治理和切换安全；MUI Renderer 提供布局、导航、弹层、JSON Schema 表单、数据展示、反馈和主题实现，1.1 同步提供 Workbench Card/Cursor，1.2 同步提供 sections/tabs/steps、分栏、异步校验和字段错误映射。Portal Generation 不会混用 MUI 与 Arco。

当前 MUI 适配器是契约基线实现。动态表单复用 RJSF + AJV 的标准 JSON Schema 语义，组件外观和高级 widget 可在不改变公共契约的前提下继续完善。

```bash
pnpm --filter @vastplan/ui-render-adapter-mui typecheck
pnpm --filter @vastplan/ui-render-adapter-mui test
```

详见《[前端门户内核](../../../docs/dev/architecture/前端门户内核.md)》和 [ADR-0052](../../../docs/dev/decisions/ADR-0052-前端门户内核与多UI设计系统插件.md)。
