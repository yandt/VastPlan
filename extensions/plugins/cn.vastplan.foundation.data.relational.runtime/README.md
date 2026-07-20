# Database Runtime 基础插件

插件 ID：`cn.vastplan.foundation.data.relational.runtime`

能力：`tool.package/foundation.data.relational.runtime`

本插件是关系数据库数据面，不是连接配置管理页，也不是承载 Go/Node/Python 插件的语言 Runtime。它最终负责 Provider 注册、本地连接池、查询、事务、健康和指标；`cn.vastplan.platform.data.relational.connection-manager` 继续拥有连接定义与 CredentialRef 生命周期。

## 当前阶段

0.4.0 已完成 Database Runtime v1 wire 契约、Provider SPI、可信运行实例 identity、加密 Material Lease 中继、统一 Pool Manager，以及 PostgreSQL/MySQL 两个真实 Provider。两个 Provider 共用 `database/sql` 执行适配层、无损 wire 值转换、稳定错误分类、事务隔离/超时、结果行数上限和池指标；Pool Manager 继续负责节点/租户/连接三级预算、调用方并发和等待队列上限、revision/generation 原子切换、旧池有界排空与关闭失败保守占额。

当前制品启动后会通过 `providers` 报告 `postgresql` 与 `mysql`，内部 Provider 已能 probe、查询、执行和开启驱动事务。`activate/query/execute/begin/commit/rollback` 尚未接入认证调用作用域、Material Lease RPC 和事务句柄路由，因此仍不会写入插件 descriptor，也不会接受外部请求；这属于明确的 fail-closed。下一阶段接入 connection-manager 发布闭环和 active-active 执行服务。

Provider options 强制使用结构化 JSON，不接受 DSN。两者都要求 `user`，默认 `tlsMode=verify-full`；`tlsMode=disable` 只有宿主显式设置 `ProviderSecurityPolicy.AllowInsecureTLS` 时才允许。PostgreSQL 使用原生 `$1` 参数占位符，MySQL 使用 `?`，Runtime 不对 SQL 文本做不安全的自动改写。

## 安全边界

- 部署时使用 native 独立进程；不与无关插件共享地址空间。
- Provider 配置不得包含 DSN、密码、token、private key 等秘密，只能引用托管 CredentialRef。
- Host 会话把首方发布者、制品摘要、节点、unit 和单次启动实例绑定为不可伪造的 Runtime audience；插件不能从 payload 自报身份。
- Provider 只保留 `MaterialSource`，需要创建物理连接时才生成一次性 X25519 密钥并取得加密 lease；Kernel 不持有私钥、看不到明文，Provider 不得缓存回调中的字节切片。
- Pool key 和快照不包含 endpoint、CredentialRef 或 material；租户、项目和连接资源只以短摘要进入内部指标。
- 新 generation 创建前按轮换重叠量预留预算；旧 generation 关闭失败时继续占额，避免实际连接数失控。
- 长期池只保存 `MaterialSource`。每条物理连接建立时临时取得 material，不生成长期密码 DSN；认证后会清空 Runtime 自己持有的候选配置。pgx、go-sql-driver/mysql 和 Go immutable string 均无法保证驱动私有连接副本中的认证字符串可原地清零，因此必须继续运行在 dedicated 可信进程，并通过关闭物理连接和 generation drain 完成凭证轮换。
- 第三方 Provider 不实现本 Go SPI，而是经未来的隔离进程和版本化 RPC 接入。

## 真实数据库集成测试

测试默认跳过，配置以下环境变量后执行 `go test ./extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime`：

- PostgreSQL：`VASTPLAN_TEST_POSTGRESQL_ENDPOINT/USER/PASSWORD/DATABASE`；
- MySQL：`VASTPLAN_TEST_MYSQL_ENDPOINT/USER/PASSWORD/DATABASE`；
- 可选：对应的 `_TLS_MODE`（默认 `verify-full`）和 `_SERVER_NAME`。

仅本地临时数据库可显式设置 `_TLS_MODE=disable`。生产配置不应放宽宿主 TLS 策略。

设计依据见 [ADR-0095](../../../docs/dev/decisions/ADR-0095-Database-Runtime多Provider连接池与集群事务.md)。
