# 平台管理访问策略

插件 ID：`cn.vastplan.foundation.security.platform-admin-access-policy`
当前制品版本：`0.6.0`

该 foundation 插件以 `per-kernel + local-ephemeral + local + direct` 运行，在设置、凭证、数据库、制品和部署能力所在 unit 内执行最终授权。它不读取 settings，不依赖远端服务，也不向浏览器暴露能力。业务插件只可调用 `stageManaged/activateManaged/abortManaged/retireManaged` 管理由凭证插件强制绑定给自己的句柄，不能继承 `put/rotate/revoke` 等管理员权限。

策略只处理五个精确平台 capability，以及全局设置/凭证的 `kernel.config.get`、数据库连接插件的 `kernel.database.probe`、Database Runtime 的 `kernel.credential.material-lease` 和 Deployment Manager 的精确宿主回调。Runtime 回调还必须由 Host 会话绑定首方发布者、制品摘要、节点、unit 和单次启动实例，只有 connection-manager 拥有的 `database.connection` 引用可以被中继。`platform.credentials.material-lease/issue` 仅允许具有非空认证身份的 `SYSTEM` caller，用户和普通插件均拒绝。未知平台操作拒绝，其他业务能力弃权；全部策略缺失时宿主仍按零校验器 fail-closed。

角色、Edge 前置检查、settings 自举策略顺序和部署要求见《[平台管理中心](../architecture/平台管理中心.md)》与 [ADR-0068](../decisions/ADR-0068-分布式平台管理中心与强类型BFF.md)。
