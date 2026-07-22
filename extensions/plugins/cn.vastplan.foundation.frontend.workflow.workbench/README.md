# UI Workbench

`cn.vastplan.foundation.frontend.workflow.workbench` 是 Portal 的基础工作流插件。Collection 现支持受治理的 Table/Page 与 Card/Cursor：共享筛选、查询取消、刷新、选择、行/卡片/批量动作和错误状态；Table 额外管理列偏好与分页，Card 额外管理稳定键去重及手动/视口增量加载。

功能插件只能通过 `@vastplan/workbench-sdk` 提交 `defineCollectionPage()` 定义；数据加载和动作处理仍由功能插件提供，视觉与状态机由本插件统一处理。

## 内部组织

`frontend/src/patterns/collection/` 是 Collection 工作台模式的内部实现目录：

- `CollectionPage`：筛选、选择、摘要和动作的顶层编排；
- `useCollectionData`：Table/Page 与 Card/Cursor 共用的加载、刷新、取消、游标和错误状态；
- `CollectionFilters`、`CollectionToolbar`、`CollectionTable`、`CollectionCards`、`CollectionPreferencesDialog`：可独立演进的组合区域；
- `density`、`filter-schema`、`preferences`：无框架私有依赖的策略与持久化辅助模块。

这里的 `CollectionTable` / `CollectionCards` 是集合工作流的受控呈现区，不是 Arco/MUI 的基础组件。基础表格和卡片分别通过 `ui.Table` / `ui.DataCard` 由渲染适配器提供。`patterns/record/` 负责 RecordDetail、MasterDetail、TreeDetail 的详情加载、主从选择、树边界、URL 恢复、窄屏与页内编辑保护；`patterns/action/` 是 Collection/Record 共用的 Page Header 动作桥。`patterns/form/` 负责 Page/Dialog/Drawer 表单、条件投影、脏状态、校验与提交；校验和提交的字段错误使用 `LocalizedText`，统一在 Workbench 解析当前语言。`secret-material.ts` 统一识别和丢弃一次性秘密，禁止材料进入 baseline 或在提交/关闭后滞留。Render Adapter 只负责把同一语义映射为各框架的表单、分栏、列表和树，不允许功能插件直接组合基础 UI 组件。

Collection 默认采用管理工作区呈现：三列筛选、左主右次操作、低对比表头、行分隔和右对齐分页。筛选字段不超过三项时不显示“查询”按钮：文本 Enter 后提交，选择类字段直接提交；达到两行时再使用“查询 + 重置”草稿模式。
