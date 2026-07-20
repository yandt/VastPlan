# Database Runtime 基础插件

插件 ID：`cn.vastplan.foundation.data.relational.runtime`

能力：`tool.package/foundation.data.relational.runtime`

本插件是关系数据库数据面，不是连接配置管理页，也不是承载 Go、Python、Node.js 或其他语言插件的通用 Runtime。它负责 Provider 注册、本地连接池、查询、事务、健康和指标；`cn.vastplan.platform.data.relational.connection-manager` 继续拥有连接定义与 CredentialRef 生命周期。

## 当前阶段

0.6.0 已完成 Database Runtime v1 wire 契约、Provider SPI、可信运行实例 identity、加密 Material Lease 中继、统一 Pool Manager、PostgreSQL/MySQL Provider、connection-manager 发布闭环、active-active 无状态执行和实例亲和事务。两个 Provider 共用 `database/sql` 执行适配层、无损 wire 值转换、稳定错误分类、结果行数上限和池指标；Pool Manager 继续负责节点/租户/连接三级预算、调用方并发和等待队列上限、revision/generation 原子切换、旧池有界排空与关闭失败保守占额。

当前制品开放 `providers/probe/activate/retire/query/execute`。只有 connection-manager 能发布、探测和退役连接；`query/execute` 拒绝用户直调，普通插件、Agent 或 Runner 还必须取得宿主投影的 `database.connection/<resourceId>` grant。管理面主动发布会命中一个 queue 副本，其他 active-active 副本首次收到该 revision 请求时，通过只允许 Runtime 调用的 `resolveRuntime` 内部操作惰性取得定义并幂等建池，因此扩容和重启不依赖伪广播。每个副本最多缓存 1 秒管理面确认；过期后同一连接的并发请求合并验证，删除会在该有界 lease 内排空本副本所有 project 池，避免未命中主动 retire 的副本无限继续服务。

`begin/commit/rollback` 返回由单次 Runtime 启动密钥加密认证的短期句柄，绑定 tenant/project/caller/ConnectionRef/owner/expiry。后续请求可落到任意副本，由受限的 Runtime-to-Runtime relay 使用 `CallTarget.instance_id` 精确转给 owner。owner 崩溃、重启或丢失本地事务时稳定返回 `database.runtime.transaction_lost`；过期自动回滚并返回 `database.runtime.transaction_expired`。连接轮换时旧事务只保留到 generation drain 上界。

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
- `allowInsecureTLS` 是 service 级重启配置，默认 `false`；只有受控测试环境可设置为 `true`，连接定义自身不能绕过该部署策略。
- `maxTransactions` 是每个 Runtime 实例的活动事务硬上限，默认 4096；达到上限时拒绝新事务，不影响无状态查询。

## 真实数据库集成测试

测试默认跳过，配置以下环境变量后执行 `go test ./extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime`：

- PostgreSQL：`VASTPLAN_TEST_POSTGRESQL_ENDPOINT/USER/PASSWORD/DATABASE`；
- MySQL：`VASTPLAN_TEST_MYSQL_ENDPOINT/USER/PASSWORD/DATABASE`；
- 可选：对应的 `_TLS_MODE`（默认 `verify-full`）和 `_SERVER_NAME`。

仅本地临时数据库可显式设置 `_TLS_MODE=disable`。生产配置不应放宽宿主 TLS 策略。

发布候选的 A5 故障矩阵使用仓库的一键入口：

```bash
./engineering/tools/database-fault-matrix.sh
```

脚本要求本机 Docker daemon 已运行，默认使用 PostgreSQL 17.10 与 MySQL 8.0.42。它在 `127.0.0.1` 临时固定端口启动两类数据库，验证真实死锁冲突、调用方/连接池预算耗尽、旧 generation 强制 drain、网络冻结与恢复、数据库停止/重启与恢复，退出时自动回收容器。可通过 `VASTPLAN_A5_POSTGRES_IMAGE` 和 `VASTPLAN_A5_MYSQL_IMAGE` 覆盖镜像以扩展版本矩阵；测试密码仅存在于临时容器和该脚本子进程环境，不写入仓库或测试日志。

设计依据见 [ADR-0095](../../../docs/dev/decisions/ADR-0095-Database-Runtime多Provider连接池与集群事务.md)。
