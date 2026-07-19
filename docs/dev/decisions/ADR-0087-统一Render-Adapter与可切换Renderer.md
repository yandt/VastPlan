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

首阶段把 Renderer 源码纳入一个已验签 Adapter 制品，先完成治理、切换和回退边界。浏览器交付协议目前只验证每插件一个入口模块；因此不能用未验签 URL 或 Blob 相对路径实现伪按需加载。

后续若需物理按需加载，必须先扩展制品清单、RuntimeSpec、Edge 内容寻址快照和浏览器 Loader，使每个 Renderer 子制品都具备独立摘要、包来源绑定、预取策略和恢复路径。该工作在交付协议完成前不得以性能优化名义绕过验签。

## 结果

- Profile 与配置中心改为编辑 Renderer 默认值，不再替换 Adapter 插件。
- 主题模板保持 Renderer 私有，避免 Arco/MUI 的原生主题对象泄漏到 Portal 或功能插件。
- 未来新增 UI 框架仅增加 Renderer，实现不改变业务插件、Shell 或 Workbench。
