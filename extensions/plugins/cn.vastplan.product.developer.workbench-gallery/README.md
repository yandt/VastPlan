# Workbench Pattern Gallery

该可选 Product 插件同时展示 `record-detail`、`master-detail` 和 `tree-detail`。它只通过 `@vastplan/workbench-sdk` 声明数据、字段、动作和表单，不导入 React、UI primitives、Arco 或 MUI。

- 记录详情：只读状态和分组字段；
- 列表主从：左侧分页/筛选列表，右侧页内动态表单，切换记录时保护未保存修改；
- 树形主从：左侧可展开层级，右侧详情和受控 Overlay。

数据保存在浏览器模块内，仅用于开发环境的 Pattern Gallery，不应进入生产 Application Composition。
