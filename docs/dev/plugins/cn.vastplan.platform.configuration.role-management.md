# Role Management Workbench

插件 ID：`cn.vastplan.platform.configuration.role-management`
当前制品版本：`0.2.0`

该平台前端插件提供权限目录、Role、Subject Binding 和审计四个 Workbench Collection 页面。页面不拥有授权状态，只调用 Portal 已绑定的 `platform.authorization` 服务；用户可见页面及动作由会话权限体验投影裁剪，所有写操作仍经过固定 BFF Route、CSRF、Management Binding、Backend Enforcer 和 Policy 状态机。

实现只依赖 `@vastplan/workbench-sdk` 与 `@vastplan/platform-admin`，不直接依赖具体 UI 框架。
