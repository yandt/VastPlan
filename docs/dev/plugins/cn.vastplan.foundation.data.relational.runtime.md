# Database Runtime 基础插件

插件 ID：`cn.vastplan.foundation.data.relational.runtime`

能力：`tool.package/foundation.data.relational.runtime`

当前制品版本：`0.14.1`

当前公开 Capability 契约：`foundation.data.relational.runtime@1.2.0`

宿主内部 Capability：`foundation.state.shared.sql.bootstrap@1.0.0`、`foundation.state.shared.sql@1.0.0`。两者只服务 Platform Control 两阶段启动，不作为普通插件可选择的数据库连接暴露。

## 职责边界

Database Runtime 是关系数据库数据面，负责 Provider、节点本地连接池、查询、事务、健康和指标；它不管理 Portal 中的连接定义，也不拥有 CredentialRef 生命周期。连接管理面仍属于 `cn.vastplan.platform.data.relational.connection-manager`，Backend Kernel 只负责通用 capability 路由、可信身份和进程生命周期。

它与 ADR-0089 的语言 Runtime Provider 不是同一概念。Database Runtime 使用 native 独立进程，不把数据库 session 和短期 material 放入 Kernel 或无关插件的地址空间。

## v1 wire 契约

机器可执行真源位于 `contracts/schemas/database/v1`，预留以下操作：

| 操作 | 语义 | 0.7.0 状态 |
|---|---|---|
| `providers` | 返回当前签名制品内冻结的 Provider descriptor | 已开放 |
| `metrics` | 输出脱敏、低基数的池/事务 counter、gauge 与健康摘要 | 仅 connection-manager 或 SYSTEM 采集器 |
| `probe` | 使用候选连接定义做连通性检查 | 仅 connection-manager 可调用 |
| `activate` / `retire` | 创建新池 generation、排空旧 generation | 已开放给 connection-manager |
| `query` / `execute` | 以 ConnectionRef 执行参数化语句 | 已开放给非用户执行主体，并强制连接 grant |
| `begin` / `commit` / `rollback` | 管理带实例亲和的不透明事务句柄 | 已开放给非用户执行主体 |

值使用显式 `null/string/int64/decimal/bool/bytes/timestamp/json` 类型。`int64` 和 `decimal` 采用字符串编码，避免 JavaScript、Go、Python 等语言之间发生精度漂移。连接载荷拒绝 DSN/URL 和疑似密码、token、private key 配置，只接受非敏感 endpoint/options 与托管 CredentialRef。

稳定错误码统一使用 `database.runtime.*`。连接诊断不会再全部压缩为“连接不可用”，而是区分部署 TLS 策略、DNS 解析、拒绝连接、连接超时、证书校验、身份验证、数据库不存在、权限不足和连接池耗尽。错误码供上层可靠映射；原始驱动文本不会进入 wire。ADR 中的 `TRANSACTION_LOST` 在 wire 上对应 `database.runtime.transaction_lost`。

凭证故障在进入数据库驱动前单独分类：引用、版本或 material 已失效使用不可重试 `database.runtime.credential_unavailable`；Vault、凭证服务或可信中继暂时不可用使用可重试 `database.runtime.credential_service_unavailable`。Runtime 不把凭证插件的远端文本、密文或引用带入诊断详情。

Runtime 在可信进程日志中记录 `operation/stage/provider/error_code/retryable/trace_id`，以及不含地址、用户名、数据库名和凭证的驱动证据，例如 PostgreSQL SQLSTATE、MySQL 错误号或网络/TLS 分类。日志禁止输出原始驱动字符串，因为驱动可能把 endpoint、账号或 DSN 拼入其中。

## Provider SPI

第一方 Provider 实现 `Descriptor/Validate/OpenPool`；Pool 实现 `Probe/Query/Execute/Begin/Stats/Close`，Transaction 实现查询、执行、提交与回滚。Registry 在注册时完成 descriptor 与配置 Schema 校验并冻结副本，拒绝重复 ID、typed-nil、`psql` ID、外部 Schema `$ref` 和注册后 descriptor 漂移。

Provider 不接收口令字段，只保留 `MaterialSource`。每次创建物理连接时通过短期回调使用 material，不能保留回调字节。该 SPI 只用于同一第一方签名制品内部；未来第三方 Provider 经独立进程和版本化 RPC 接入。

## 可信 material 路径

Backend Host 从已验签 `LaunchPolicy` 生成 host-only Runtime identity，绑定插件 ID、发布者、版本、制品 SHA-256、节点、service unit 和每次启动随机实例。完整 identity 不进入 wire；插件只接收其非秘密 audience 摘要。Database Runtime 每次使用凭证生成一次性 X25519 接收密钥，经声明的 `kernel.credential.material-lease` 请求加密信封。

Kernel 只允许当前认证会话调用，并只为首方 Database Runtime、中继 connection-manager 所拥有的 `database.connection` 引用；它不创建接收私钥，也不解封 material。跨服务凭证插件继续只接受 transport-trusted `SYSTEM` 调用并把同一 audience 写入 AAD。Runtime 校验 tenant、audience、完整 CredentialRef、TTL 和 GCM 后在同步回调中使用并清零明文。错误 audience、跨 tenant、跨 owner、过期 lease 和伪造 caller 均 fail-closed。

## 发布、active-active 与执行授权

connection-manager 以持久化 outbox 发布完整的非敏感 `ConnectionSpec` 和托管 CredentialRef。queue 路由的一次主动发布只保证一个 Runtime 副本收敛；其他副本在首次收到指定 revision 的执行请求时，反向调用受限 `platform.database/resolveRuntime` 并幂等激活本地池。Runtime 对成功确认只缓存 1 秒，并按 tenant + connection 合并并发验证；管理面删除或 revision 不一致会在该有界 lease 内触发本副本所有 project 池排空，未收到主动 retire 的副本不会无限继续服务。管理面不可达且 lease 已过期时 fail-closed。

用户不能直接调用 SQL。插件、Agent 和 Runner 除可信 caller 外，还必须在宿主投影上下文中持有名为 `database.connection/<resourceId>`、scope 为 tenant/project 的连接 grant；Runtime 不接受 payload 自报授权。SYSTEM 调用仅用于可信平台内部执行。

## 事务亲和与故障语义

`begin` 在当前 Runtime 实例取得一个 generation lease 和驱动事务，返回 `vptx1` 不透明句柄。句柄声明使用每次进程启动随机生成的 AES-256-GCM 密钥加密认证，绑定 tenant、project、caller kind/ID、ConnectionRef、owner Runtime audience、随机 transaction ID 和 expiry；不包含 SQL、参数、endpoint、CredentialRef 或 material。句柄仅暴露非秘密的 owner 路由前缀，调用方不得解析或修改。

后续事务请求仍可进入 active-active queue 的任意健康副本。非 owner 副本先校验调用主体和连接 grant，再以内部 `transactionRelay` 精确调用 owner 的 `CallTarget.instance_id`；只有精确 Runtime caller 可使用 relay。owner 会再次验证加密句柄及原始 scope/caller；查询、执行和提交均重新校验当前连接 grant，原 caller 即使 grant 已撤销仍可回滚。实例不存在、重启后密钥变化或本地状态丢失统一返回可重试的 `database.runtime.transaction_lost`，调用方只能从整个事务边界按幂等策略重试；句柄过期返回不可重试的 `database.runtime.transaction_expired` 并自动回滚。

事务保持旧 pool generation 的 lease。连接/凭证 revision 轮换后，旧事务可在 Pool Manager 的 drain 窗口内结束；超过窗口会关闭旧 generation，Runtime 自动清理事务并返回 `transaction_lost`。每个实例默认最多 4096 个活动事务，可通过 service 级 `maxTransactions` 下调或在受控容量评估后上调。

当前首方 PostgreSQL/MySQL Provider 使用 Go，是因为本阶段与 Go Pool Manager、`database/sql` 及现有驱动集成的成本最低；这不是语言限制。未来 Provider 按效率、生态和场景选型，Python、Node.js、Rust、Java 等实现经专用可信 Provider Host 和版本化 RPC 接入，物理池仍留在其语言进程。

## 指标与告警

`metrics` 是平台级监控的版本化数据面，不是面向用户的查询接口。它返回绝对值 counter/gauge，名称遵循 Prometheus/OpenTelemetry 约定：`*_total` 为 counter，其余为 gauge。涵盖连接池 open/idle/in-use/max、等待、预算/队列拒绝、forced drain、关闭失败，以及事务 active/capacity/begin/commit/rollback/expired/lost/rejected。

只能使用 `provider` 等低基数标签；禁止 tenant、project、连接标识（包括 hash）、endpoint、SQL、CredentialRef、事务句柄和 Runtime audience。采集器处理 Runtime 重启时必须识别 counter reset。运行状态为 `idle/ready/degraded/unavailable`；建议至少对 unavailable、资源拒绝或超时的增量、事务使用率达到 80%、事务丢失和关闭失败增量建立告警。告警发送与长期存储属于未来监控插件，不在 Database Runtime 内实现。

## 当前限制与下一阶段

事务亲和、超时/崩溃丢失语义、凭证轮换 drain、真实 PostgreSQL/MySQL 故障矩阵和标准指标出口均已完成。后续重点是第三方 Provider Host，以及由独立监控插件实现的 Prometheus/OTel 采集适配、告警发送与长期存储；完整路线见 [ADR-0095](../decisions/ADR-0095-Database-Runtime多Provider连接池与集群事务.md)。

按 [ADR-0194](../decisions/ADR-0194-平台控制数据库与声明式数据分层.md)，同一签名制品已增加 Record Store、Schema Controller 和 SQL Shared State 内部模块；它们不是新的独立进程或普通应用插件。P3b 已完成声明式 CRUD、Batch、实例亲和 UnitOfWork、幂等/Outbox 账本、安全增量迁移、数据库迁移锁、迁移账本和 SQL Shared State 的 PostgreSQL/MySQL 实现。

P3c 已补齐破坏性迁移：`data.migration.v1` 文件由签名 Manifest 固定摘要，并在同步目录时继续绑定已验证 Plugin Inventory 与 Artifact SHA；Schema Controller 需要 leader、备份和审批三类宿主 evidence，只执行声明可安全重试的受限 SQL，成功账本同时记录 migration ID。

这些模块已经进入同一 Database Runtime Manifest 和进程主入口。Bootstrap 候选池不会作为普通 ConnectionRef 发布，而是同时绑定 Shared State 与平台 Record Store 的窄会话端口；`storage.kind=platform-control` 只有在已验证 Inventory 中、调用插件与模型 owner 一致时才可执行。PostgreSQL 由可信 Bootstrap Profile 把限定 schema 注入连接 `search_path`，MySQL 继续要求 schema 等于 database，因此 DataModel 表、迁移账本、幂等表和 Outbox 都落在保留平台命名空间。平台库 generation 切换会关闭旧池并使旧事务稳定进入 `transaction_lost`，不会回退本机 JSON。真实 PostgreSQL/MySQL 矩阵仍需在本机 Docker daemon 恢复后补跑。
