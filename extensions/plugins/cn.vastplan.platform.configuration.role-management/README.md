# Role Management

`cn.vastplan.platform.configuration.role-management` 是平台授权治理的 Workbench 前端插件。它只通过 Portal Management Binding 调用 `platform.authorization`，不保存角色、不签发快照，也不参与 Backend 最终判定。

页面包括权限目录、Role revision、Subject Binding 和授权审计。Role/Binding 支持创建、Draft 编辑、提交、不同主体审批、发布、退役；Published Binding 支持即时撤权。页面和动作声明精确 `requiredPermissions`，Portal 依据短期会话体验投影隐藏无权入口，但 BFF 和 Backend Enforcer 仍会独立复核。

该插件使用 TypeScript 与统一 Workbench Collection/Form 契约，不直接依赖 Arco 或 MUI，因此可随 Renderer 与 Shell 在线切换。
