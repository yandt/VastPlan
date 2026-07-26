# Ant Design Renderer

插件 ID：`cn.vastplan.foundation.frontend.render.adapter.antd`

当前制品版本：`1.0.1`

该基础插件是统一 `ui.render.adapter` Catalog 下的受信任 React Renderer Library，不是独立 Adapter，也不是新的 Runtime Engine。Portal 只在活动 Platform Profile 选择 `renderer=antd` 时校验并下载它。

主要能力：

- 完整实现 `@vastplan/ui-primitives` 的布局、导航、数据、反馈、图标和动态表单表面；
- 使用 Ant Design `ConfigProvider`、原生明暗算法和 locale bridge；
- 使用 `@ant-design/cssinjs` 将运行时样式注入 Portal Shadow Root，Overlay 也限制在 Portal 边界内；
- 使用 RJSF Ant Design widgets/templates，并直接引用 RJSF Form 子入口和共享 CSP Validator，避免引入测试 Registry 与运行时代码生成；
- 原生图标逐个子路径导入，构建门禁限制图标数量和 Renderer bundle 体积；
- 与 Arco、MUI 共享同一语义 UI Contract。功能插件不得直接导入 Ant Design。

默认本地 Platform Profile 首选 Ant Design，同时保留 Arco 与 Material UI 供管理员或用户按允许范围切换。切换通过新的 Host Epoch 和页面刷新完成，不会在同一 React 树混用多个 UI 框架。

架构决策见 [ADR-0087](../decisions/ADR-0087-统一Render-Adapter与可切换Renderer.md) 与 [ADR-0159](../decisions/ADR-0159-Ant-Design首选Renderer与按需交付.md)。
