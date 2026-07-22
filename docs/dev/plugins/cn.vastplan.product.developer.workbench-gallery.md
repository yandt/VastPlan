# Workbench Pattern Gallery

`cn.vastplan.product.developer.workbench-gallery` 是开发环境可选择的 Product Application 插件，用真实 Portal Generation 展示 Workbench 的三种记录模式：

- `record-detail`：固定单记录的分区详情；
- `master-detail`：左侧筛选/分页列表，右侧 page-surface 编辑器；
- `tree-detail`：左侧有界资源树，右侧详情和 JSON Overlay。

它只依赖 `@vastplan/workbench-sdk`，不导入 React、`ui-primitives`、Arco 或 MUI。数据仅保存在浏览器模块内；生产 Application Composition 不应安装该 Gallery。

开发平台通过 Application Composition 安装它，而不是放入 Platform Profile。它使用 `product` 命名空间，是因为可信 Portal 明确拒绝 `cn.vastplan.example.*` 开发制品；Gallery 仍遵守与普通应用插件相同的签名、制品锁、运行描述和 Workbench 边界。
