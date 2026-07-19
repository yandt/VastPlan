# VastPlan Portal Shell

首方 `ui.structure.shell` 基础插件。它是 Portal 唯一的结构基础：固定 Slot、页面/导航归并和活动路径，同时提供 `standard` 与 `top-navigation` 两个受治理模板。

功能插件只能通过 `addPage` 与 `addShellContribution` 填充标准 Slot，不能感知或选择模板。Platform Profile 决定默认模板与允许范围；用户偏好只在允许范围内保存于浏览器本地。
