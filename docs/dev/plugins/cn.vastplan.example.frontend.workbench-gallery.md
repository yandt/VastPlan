# Workbench Pattern Gallery

`cn.vastplan.example.frontend.workbench-gallery` 是开发环境可选择的 Example Application 插件，用真实 Portal Generation 展示 Workbench 的三种记录模式：

- `record-detail`：固定单记录的分区详情；
- `master-detail`：左侧筛选/分页列表，右侧 page-surface 编辑器；
- `tree-detail`：左侧有界资源树，右侧详情和 JSON Overlay。

它只依赖 `@vastplan/workbench-sdk`，不导入 React、`ui-primitives`、Arco 或 MUI。数据仅保存在浏览器模块内；生产 Application Composition 不应安装该 Gallery。

开发平台通过 testing/workspace 制品和 Application Test Release 安装它，而不是放入 Platform Profile。Backend 组合根只在开发策略下允许 `example` 身份，生产 Portal 继续 fail-closed；Gallery 仍遵守与普通应用插件相同的签名、制品锁、运行描述和 Workbench 边界。
