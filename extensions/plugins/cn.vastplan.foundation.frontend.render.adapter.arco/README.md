# Arco Design System Plugin

`cn.vastplan.foundation.frontend.render.adapter.arco` 是统一 Render Adapter 的内部 Arco Renderer 模块。它实现 `@vastplan/ui-primitives` 的语义组件、动态表单与主题映射；1.1 增加 Workbench Card/Cursor，1.2 增加同一完整 Schema 上的 sections/tabs/steps、分栏与 Workbench 字段状态映射，1.3 增加受治理的 `secretMaterial` password 呈现。Portal 只会在 Adapter 目录选中 `arco` 时下载它，不单独贡献 `ui.render.adapter` 或作为 Portal 基础插件被选择。

Portal 只选择 `cn.vastplan.foundation.frontend.render.adapter`。该 Adapter 在其受治理 Renderer 目录中使用本源码；同一 Portal Generation 仍只运行一个 Renderer。

生产构建直接使用 RJSF 导出的 `Form` 子路径，不经过会导入测试 Registry 的根入口；Schema 校验始终由 `@vastplan/rjsf-csp-validator` 提供，不安装或打包 `@rjsf/validator-ajv8`。

功能插件只能从 `@vastplan/ui-primitives` 获取语义化组件与 hooks，不能直接导入 Arco、修改全局 CSS 或管理顶级弹窗栈。Portal 内核在执行远程模块前验证该插件为已签名第一方制品、`uiContract` 兼容且基础 UI 能力完整。Arco 的 Overlay holder 位于当前 Portal Shadow DOM 内，不使用全局 `document.body` 服务。

动态表单在插件内部使用 RJSF 6 + CSP 安全的解释式校验器，接收 JSON Schema Draft 7 与可选 `uiSchema`。插件提供 Arco 文本、多行、数字、布尔、枚举/多选、日期、对象、数组、组合选择、增删排序、错误摘要和自定义凭证引用 widget；默认值、组合、条件与同步校验均遵循标准 JSON Schema。服务端校验通过可取消的异步 validator 注入，浏览器无需 `unsafe-eval`。

凭证字段必须同时声明 `format: vastplan-credential-ref` 和 `writeOnly: true`，值只能是 `credential://...` 引用；`ui:widget: secretRef` 只控制 Arco 呈现，不是后端放行依据。表单禁止远程 `$ref`，公共 Portal SDK 不暴露 RJSF 或 Arco 类型。

Arco 采用编译期按需加载：`arco-components.ts` 只暴露实际使用的组件和图标直接 ESM 入口，`arco-styles.ts` 只合并这些组件的传递样式闭包。插件仍构建为一个可签名 ESM，不产生运行时未锁定 chunk。`pnpm run build:frontend:plugins` 会拒绝组件与样式不一致、全量 Arco 入口或超过 1,700,000 字节预算的产物。

```bash
pnpm --filter @vastplan/ui-render-adapter-arco typecheck
pnpm --filter @vastplan/ui-render-adapter-arco test
pnpm run build:frontend:plugins
```

详见《[前端门户内核](../../../docs/dev/architecture/前端门户内核.md)》、[ADR-0052](../../../docs/dev/decisions/ADR-0052-前端门户内核与多UI设计系统插件.md)、[ADR-0064](../../../docs/dev/decisions/ADR-0064-Portal语义组件契约与动态表单运行时.md)、[ADR-0065](../../../docs/dev/decisions/ADR-0065-通用JSON-Schema表单与Arco主题适配.md) 与 [ADR-0066](../../../docs/dev/decisions/ADR-0066-Arco按需构建与单文件制品边界.md)。
