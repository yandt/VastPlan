# UI Workbench

`cn.vastplan.foundation.frontend.workflow.workbench` 是 Portal 的基础工作流插件。V1 只实现受治理的表格集合：筛选、分页、列可见性/顺序、选择与行/批量操作。

功能插件只能通过 `@vastplan/workbench-sdk` 提交 `defineCollectionPage()` 定义；数据加载和动作处理仍由功能插件提供，视觉与状态机由本插件统一处理。
