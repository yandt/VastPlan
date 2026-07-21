# VastPlan Portal Shell

首方 `ui.structure.shell` 基础插件。它是 Portal 唯一的结构基础：固定 Slot、页面/导航归并和活动路径，并提供受签名清单约束的 Shell Library Catalog。`1.1.0` 将 `standard` 与 `top-navigation` Library 精确锁定到各自的 `1.1.0` 制品。

功能插件只能通过 `addPage` 与 `addShellContribution` 填充标准 Slot，不能感知或选择布局。`standard` 与 `top-navigation` 是独立、延迟加载的已签名 Library 制品；浏览器只下载当前选中项。Platform Profile 决定默认值与允许范围；用户切换先在后台装配候选 Portal Generation，成功后才原子替换当前页面，失败保留原 Library。
