# Standard Shell Composition

本基础插件是 Portal 的 Slot 与导航单一管理层。它分开管理全局 `shell.*` 与活动页面 `page.*`，固定 `shell.navigation.start|center|end` 等区域，并把导航规范为有界的 `zone → group → page`；不包含 CSS，也不决定菜单的视觉位置。

页面通过 `groupID` 引用分组。分组描述符由 Platform Profile 的 `composition.config.navigationGroups` 提供，包含 `id / label / zone / icon / order`；未分组页面进入所属 zone 的内建同名组，未知分组和跨 zone 引用会被拒绝。功能插件只能注册页面并填充 `page.*`；全局 Shell 贡献由宿主单独校验来源。布局插件只消费本插件生成的标准化模型，因此更换布局不会改变功能插件契约。
