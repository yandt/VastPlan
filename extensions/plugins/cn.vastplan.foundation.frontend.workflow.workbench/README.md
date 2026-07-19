# UI Workbench

`cn.vastplan.foundation.frontend.workflow.workbench` 是 Portal 的基础工作流插件。V1 只实现受治理的表格集合：筛选、分页、列可见性/顺序、选择与行/批量操作。

功能插件只能通过 `@vastplan/workbench-sdk` 提交 `defineCollectionPage()` 定义；数据加载和动作处理仍由功能插件提供，视觉与状态机由本插件统一处理。

## 内部组织

`frontend/src/patterns/collection/` 是 Collection 工作台模式的内部实现目录：

- `CollectionPage`：加载、刷新、选择和动作的状态编排；
- `CollectionFilters`、`CollectionToolbar`、`CollectionTable`、`CollectionPreferencesDialog`：可独立演进的组合区域；
- `density`、`filter-schema`、`preferences`：无框架私有依赖的策略与持久化辅助模块。

这里的 `CollectionTable` 是筛选、列偏好、操作与分页工作流中的“表格组合区”，不是 Arco/MUI 的基础表格。基础表格仍通过 `ui.Table` 由渲染适配器提供。未来的表单、详情和 Overlay 工作台模式应新增到 `patterns/form/`、`patterns/record/`、`patterns/overlay/`，不得重新堆回入口文件或让功能插件直接组合基础 UI 组件。
