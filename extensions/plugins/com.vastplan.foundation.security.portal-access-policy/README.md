# Portal Access Policy

首方基础权限插件。它只根据受验证 `CallContext` 的用户角色授权 Portal Composer，且仅允许 Composer 调用两个受限内核服务；未匹配请求保持 fail-closed。
