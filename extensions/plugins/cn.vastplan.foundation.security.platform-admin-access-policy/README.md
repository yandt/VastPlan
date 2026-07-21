# Platform Admin Access Policy

本地、无状态、默认拒绝的平台管理权限插件。它只处理 `platform.settings`、`platform.credentials`、`platform.database`、`platform.artifacts.repository`、可信宿主 `platform.credentials.material-lease` 及精确的基础插件宿主回调；其他能力一律弃权。

Node Portal Kernel 会先执行同一角色粒度的路由检查，能力所在 Backend unit 再由本插件作最终授权。`platform.admin` 可管理全部平台资源；各领域的 `.read/.write/.rotate/.revoke/.probe` 角色遵循最小权限。系统设置写入继续受 bootstrap-policy 最高优先级保护，只有 `platform.admin` 被映射为直接登录管理员。

`platform.credentials.material-lease/issue` 不属于管理员页面能力，只允许认证后的 `SYSTEM` caller；用户和普通插件一律拒绝。
