# Workbench Pattern Gallery

该可选 Example 插件同时展示 `record-detail`、`master-detail` 和 `tree-detail`。它只通过 `@vastplan/workbench-sdk` 声明数据、字段、动作和表单，不导入 React、UI primitives 或具体 UI 框架。

它还通过 Manifest `extensions` 接入个人中心的版本化 `frontend.page` 扩展点，并注册“账户扩展示例”页面。该路径用于验证扩展插件只依赖公开契约，不导入个人中心源码，同时由可信 Catalog 与 Portal Generation 核对页面 ID 和导航目标。

- 记录详情：只读状态和分组字段；
- 列表主从：左侧分页/筛选列表，右侧页内动态表单，切换记录时保护未保存修改；
- 树形主从：左侧可展开层级，右侧详情和受控 Overlay。

数据保存在浏览器模块内，仅用于开发环境的 Pattern Gallery，不应进入生产 Application Composition。
