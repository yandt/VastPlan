# Portal Access Policy

首方基础权限插件。它只根据受验证 `CallContext` 的用户角色授权 Portal Composer，并只允许精确 Composer 身份调用 Catalog、制品引用与 `kernel.state.shared.get/create/update` 窄内核服务；未匹配请求保持 fail-closed。
