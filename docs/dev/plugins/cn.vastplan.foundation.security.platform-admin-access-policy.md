# 平台管理访问策略

插件 ID：`cn.vastplan.foundation.security.platform-admin-access-policy`
当前制品版本：`0.11.0`

该 foundation 插件以 `per-kernel + local-ephemeral + local + direct` 运行，在设置、凭证、数据库、制品和部署能力所在 unit 内执行最终授权。它不读取 settings，不依赖远端服务，也不向浏览器暴露能力。业务插件只可调用 `stageManaged/activateManaged/abortManaged/retireManaged` 管理由凭证插件强制绑定给自己的句柄，不能继承 `put/rotate/revoke` 等管理员权限。

策略处理五个管理 capability、Database Runtime 数据面，以及全局设置/凭证的 `kernel.config.get`、Database Runtime 的 `kernel.credential.material-lease` 和 Deployment Manager 的精确宿主回调。Deployment Manager 可读取 Catalog 并生成精确制品锁，但不得发布制品或导出 Bundle。只有精确的仓库插件可调用 `platform.artifacts.storage.*` 的 `probe/provision/describe/migrate/release`，用户与其他插件不能直达 Storage Provider。只有 connection-manager 可调用 Runtime 的 `activate/retire/probe`，只有精确 Runtime caller 可反向调用管理面的内部 `resolveRuntime` 和 Runtime 间 `transactionRelay`；用户不能直接调用 `query/execute/begin/commit/rollback`，非用户执行主体仍须由 Runtime 二次校验连接 grant 和事务句柄绑定。Runtime Material Lease 身份继续绑定首方发布者、制品摘要、节点、unit 和单次启动实例，且只能中继 connection-manager 拥有的 `database.connection` 引用。未知平台操作拒绝，其他业务能力弃权；全部策略缺失时宿主仍按零校验器 fail-closed。

角色、Edge 前置检查、settings 自举策略顺序和部署要求见《[平台管理中心](../architecture/平台管理中心.md)》与 [ADR-0068](../decisions/ADR-0068-分布式平台管理中心与强类型BFF.md)。
