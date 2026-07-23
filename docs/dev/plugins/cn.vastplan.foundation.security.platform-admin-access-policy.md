# 平台 Workload 访问策略

插件 ID：`cn.vastplan.foundation.security.platform-admin-access-policy`
当前制品版本：`0.30.0`

0.25.0 在既有 `kernel.config.credential-ref`、Application/Profile 配置激活、凭证 delegated 和 Service Hot 窄授权上，增加独立资源控制器访问：只有精确 plugin-settings、同租户且目标为 `configuration.resource-controller + configuration.resource.* + list/get/prepare/commit/abort/status` 时放行。业务插件、用户、错误扩展点或伪造 capability 均不能调用目标插件的内部配置事务端口。

0.26.0 增加 `configuration.scoped-resolver/configuration.scoped` 的运行时读取授权：只有认证 plugin caller 可调用 `resolve/watchRevision`，用户和错误扩展点一律拒绝；resolver 自身继续按活动 Catalog、caller plugin、tenant 和 subject 做对象级复核，策略放行不等于能读取任意配置。

0.27.0 为 global-settings、plugin-settings 和 Deployment Manager 增加精确 `kernel.state.shared.get/create/update` workload grant。Portal Composer 的同类授权由 Portal 专属访问策略负责。其他插件、用户、错误 capability 或 delete/list 仍拒绝；Shared State 宿主继续从认证身份派生 tenant、plugin ID 与 RuntimeScope，策略不会允许调用方自报存储身份。

0.28.0 为 Credentials 增加相同的最小 `get/create/update` Shared State grant，并删除其不再使用的 `kernel.config.get`。凭证插件仍可消费一次性 ConfigurationAuthority；delete/list 和其他插件身份继续拒绝。

0.29.0 将 Credentials 收窄为 `get/list + fenced.create/update/delete`：普通无 fence mutation 不再授权。当前 Unit Leader evidence 由可信 Host 校验，插件不可见 epoch/token；其他 Shared State 插件继续使用原有 CAS grant，不因本次变更获得 delete/list。

0.30.0 允许 API Exposure 控制面向 Repository 的独立评估报告数据面安装短时单次 Ticket；普通制品 Ticket 与评估报告 Ticket 仍由 Repository 按资源路径分别收窄。

该 foundation 插件以 `per-kernel + local-ephemeral + local + direct` 运行，只治理系统与插件 workload 的精确回调。用户管理操作已全部交给签名 Permission Catalog 与优先级更高的 `authorization-enforcer`；本插件即使看到 `platform.admin`、精确 permission code 或 `is_admin` 也不会放行用户。

策略处理五个管理 capability、Database Runtime 数据面，以及平台状态插件的精确 Shared State 调用、凭证配置读取、Database Runtime 的 `kernel.credential.material-lease` 和 Deployment Manager 的精确宿主回调。Deployment Manager 可读取 Catalog 并生成精确制品锁，但不得发布制品或导出 Bundle。经 addressing 认证且 caller 固定为 `node-agent/<nodeID>` 的内核服务只能发布自身 `assignment-active` 引用，`bootstrap-inventory/<repositoryId>` 只能发布匹配 ID 的 Seed/LKG。制品 GC 的 plan/status 只需 `platform.artifacts.read`，quarantine/sweep 使用独立 `platform.artifacts.gc`，不能由 lifecycle 或 migrate 角色隐式取得。只有精确的仓库插件可调用 `platform.artifacts.storage.*` 的 `probe/provision/describe/migrate/release`，用户与其他插件不能直达 Storage Provider。只有 connection-manager 可调用 Runtime 的 `activate/retire/probe`，只有精确 Runtime caller 可反向调用管理面的内部 `resolveRuntime` 和 Runtime 间 `transactionRelay`；用户不能直接调用 `query/execute/begin/commit/rollback`，非用户执行主体仍须由 Runtime 二次校验连接 grant 和事务句柄绑定。Runtime Material Lease 身份继续绑定首方发布者、制品摘要、节点、unit 和单次启动实例，且只能中继 connection-manager 拥有的 `database.connection` 引用。未知平台操作拒绝，其他业务能力弃权；全部策略缺失时宿主仍按零校验器 fail-closed。

用户 operation-role 表和 `platform.admin` 兼容旁路已在 B6 删除。保留的硬编码项均是 caller 精确匹配的 workload grant，例如 Deployment Manager 宿主回调、制品 Storage Provider、Database Runtime 和 Material Lease；新增用户权限必须进入所属插件 Manifest，不能加入本策略。角色、Provider Protocol、Node BFF 前置检查、settings 自举策略顺序和部署要求见《[在线角色与权限治理](../architecture/在线角色与权限治理.md)》、《[平台管理中心](../architecture/平台管理中心.md)》与 [ADR-0107](../decisions/ADR-0107-插件权限目录与系统管理授权治理.md)。
