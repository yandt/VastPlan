# ADR-0087 统一 Render Adapter 与可切换 Renderer

- 状态：已采纳（实施中）
- 日期：2026-07-20
- 关联：[ADR-0052](ADR-0052-前端门户内核与多UI设计系统插件.md)、[ADR-0085](ADR-0085-渲染适配器主题模板契约.md)、[ADR-0086](ADR-0086-单Shell插件与可切换布局模板.md)

## 决策

Portal 只装配一个首方 `ui.render.adapter`：`cn.vastplan.foundation.frontend.render.adapter`。Arco、MUI 和未来框架成为这个可信 Adapter 内部的 **Renderer**，而不再是相互竞争的基础插件。

Profile 使用下列受限目录治理 Renderer：

```json
{
  "defaultRenderer": "arco",
  "allowedRenderers": ["arco", "mui"],
  "userSelectable": true,
  "rendererOptions": {
    "arco": { "themeTemplate": "light" },
    "mui": { "themeTemplate": "light" }
  }
}
```

功能插件继续只使用 `@vastplan/ui-primitives`；不得选择、探测或导入框架实现。Shell 与 Workbench 同样只消费语义组件契约。

用户选择仅在 `userSelectable=true` 且值属于 `allowedRenderers` 时有效，存储在 tenant/portal/adapter 隔离的本地偏好中。切换 Renderer 必须重新校验 Renderer 目录、能力集、主题目录和 UI Contract，创建新的 Portal Generation 后刷新页面；禁止在同一 DOM 或 React 树中同时运行两个框架 Provider。失败时现有可信页面不被替换。

## 交付与性能边界

Renderer 保持为 Adapter 管辖的首方内部模块：每个框架拥有独立、已验签的前端制品入口，Adapter 主模块只携带目录和模块引用。RuntimeSpec 锁定这些模块的摘要与包来源，但 Portal 只在确定用户/Profile 的 Renderer 后，通过既有摘要校验 Loader 获取该模块。未选 Renderer 不下载、不执行，也不进入 React 树。

这不是普通 `import()` 分包：Portal 会将已校验字节从 Blob URL 执行，普通相对路径分包既不能稳定解析，也会绕过内容摘要验证。Renderer 模块必须继续作为 RuntimeSpec 中的受控模块存在；禁止使用任意 URL、未锁定动态 import 或将框架选择权交给功能插件。

## 结果

- Profile 与配置中心改为编辑 Renderer 默认值，不再替换 Adapter 插件。
- 主题模板保持 Renderer 私有，避免 Arco/MUI 的原生主题对象泄漏到 Portal 或功能插件。
- 未来新增 UI 框架仅增加 Renderer，实现不改变业务插件、Shell 或 Workbench。
- 首屏只传输当前 Renderer 的框架代码；用户切换 Renderer 后刷新新一代 Portal，再加载对应模块。
