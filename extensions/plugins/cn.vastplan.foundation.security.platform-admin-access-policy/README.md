# Platform Admin Access Policy

本地、无状态、默认拒绝的平台管理权限插件。它只处理 `platform.settings`、`platform.credentials`、`platform.database`、`platform.artifacts.repository`、可信宿主 `platform.credentials.material-lease` 及精确的基础插件宿主回调；其他能力一律弃权。

Node Portal Kernel 会先执行同一角色粒度的路由检查，能力所在 Backend unit 再由本插件作最终授权。各领域的 `.read/.write/.rotate/.revoke/.probe` 权限遵循最小权限，测试目标绑定单独要求 `platform.deployment.test-target`。`platform.admin` 只作为在线角色 B1→B6 迁移期的兼容映射；新能力不得加入这个旁路，最终由签名 Permission Catalog、Policy Snapshot 和本地 `authorization-enforcer` 取代。系统设置写入仍受 bootstrap-policy 最高优先级保护。

`platform.credentials.material-lease/issue` 不属于管理员页面能力，只允许认证后的 `SYSTEM` caller；用户和普通插件一律拒绝。
