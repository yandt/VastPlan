# Ant Design Renderer Plugin

`cn.vastplan.foundation.frontend.render.adapter.antd` 是统一 Render Adapter 的内部 Ant Design Renderer 模块。它实现完整 `@vastplan/ui-primitives` 语义组件面，并通过 `@rjsf/antd` 与 VastPlan CSP Validator 渲染动态表单。

Portal 只会在 Adapter 目录选中 `antd` 时下载本模块；Arco、MUI 与 Ant Design 不会同时进入同一 React 树。深浅主题使用 Ant Design 原生 Algorithm，弹层统一挂载到 Portal Shadow DOM 内，功能插件不能直接导入 `antd`。

```bash
pnpm --filter @vastplan/ui-render-adapter-antd typecheck
pnpm --filter @vastplan/ui-render-adapter-antd test
```

详见《[前端门户内核](../../../docs/dev/architecture/前端门户内核.md)》与 [ADR-0159](../../../docs/dev/decisions/ADR-0159-Ant-Design首选Renderer与按需交付.md)。
