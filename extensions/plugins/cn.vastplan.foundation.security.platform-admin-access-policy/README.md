# Platform Admin Access Policy

本地、无状态、默认拒绝的平台管理权限插件。它只处理 `platform.settings`、`platform.credentials`、`platform.database`、`platform.artifacts.repository`、可信宿主 `platform.credentials.material-lease` 及精确的基础插件宿主回调；其他能力一律弃权。

Node Portal Kernel 会先执行精确 permission code 的路由预检，能力所在 Backend unit 再由 `authorization-enforcer` 根据签名 Policy Snapshot 作用户最终授权。本插件只保留系统/插件 workload 的精确 caller grant，并对落入旧平台能力的用户请求拒绝；`platform.admin`、`is_admin` 和精确用户角色都不会在本插件中放行。系统设置写入仍受 bootstrap-policy 最高优先级保护。

`platform.credentials.material-lease/issue` 不属于管理员页面能力，只允许认证后的 `SYSTEM` caller；用户和普通插件一律拒绝。
