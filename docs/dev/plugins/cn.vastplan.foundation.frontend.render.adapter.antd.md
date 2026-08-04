# Ant Design Renderer

插件 ID：`cn.vastplan.foundation.frontend.render.adapter.antd`

当前制品版本：`6.1.7`

该基础插件是统一 `ui.render.adapter` Catalog 下的受信任 React Renderer Library，不是独立 Adapter，也不是新的 Runtime Engine。Portal 只在活动 Platform Profile 选择 `renderer=antd` 时校验并下载它。

主要能力：

- 完整实现 `@vastplan/ui-primitives` 的布局、导航、数据、反馈、图标和动态表单表面；
- 使用 Ant Design `ConfigProvider`、原生明暗算法和 locale bridge；
- 使用 `@ant-design/cssinjs` 将运行时样式注入 Portal Shadow Root，Overlay 也限制在 Portal 边界内；
- 使用 RJSF Ant Design widgets/templates，并直接引用 RJSF Form 子入口和共享 CSP Validator，避免引入测试 Registry 与运行时代码生成；
- 原生图标逐个子路径导入，构建门禁限制图标数量和 Renderer bundle 体积；
- Descriptions 原生透传响应式列数和条目跨列规则，不再把 `xs` 列数错误固化到所有视口；
- FormDialog 使用 Ant 原生垂直居中与 `95vh` 高度上限，保留视口边距并分离 Header、滚动 Body 与 Footer；动态表单分区支持嵌套对象继承列数、紧凑行距和手机端 Label 堆叠；
- 实现通用语义 UI Contract。功能插件不得直接导入 Ant Design。

默认本地 Platform Profile 只允许 Ant Design。通用 Adapter 仍保留多 Renderer Catalog 与 Host Epoch 切换能力，但在第二个完整实现通过门禁前不向管理员或用户显示框架切换。

架构决策见 [ADR-0087](../decisions/ADR-0087-统一Render-Adapter与可切换Renderer.md) 与 [ADR-0159](../decisions/ADR-0159-Ant-Design首选Renderer与按需交付.md)。
