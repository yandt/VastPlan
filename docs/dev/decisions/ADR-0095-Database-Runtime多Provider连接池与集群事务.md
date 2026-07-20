# ADR-0095 Database Runtime、多 Provider 连接池与集群事务

- 状态：已采纳
- 日期：2026-07-20
- 关联：[ADR-0045 插件实例化策略与服务集群化边界](ADR-0045-插件实例化策略与服务集群化边界.md)、[ADR-0089 Runtime Provider 与共享 Host 池](ADR-0089-Runtime-Provider与共享Host池.md)、[ADR-0092 业务插件拥有托管凭证生命周期](ADR-0092-业务插件拥有托管凭证生命周期.md)、[ADR-0093 可信宿主加密 Material Lease](ADR-0093-可信宿主加密Material-Lease.md)

## 背景

数据库连接基础插件已经能够管理租户隔离的连接定义和托管凭证引用，并把 `probe` 委托给可信宿主；当前宿主只具备测试适配器，还没有可生产使用的数据库 Provider、连接池、配额、健康状态和事务亲和。

把 PostgreSQL 驱动和连接池直接编入 Kernel 可以较快工作，却会把特定数据库、驱动升级和大量连接状态带入微内核。把每条连接交给普通业务插件自行建池，则会复制安全、限流、凭证轮换和集群事务逻辑。系统需要保持 Kernel 的通用性，同时允许 PostgreSQL、MySQL 以及未来 SQL Server、Oracle 等 Provider 独立演进。

本文的 `Database Runtime` 是数据库访问基础插件，不是 ADR-0089 中承载 Go/Node/Python 代码的语言 Runtime Provider。

## 决策

### 1. 三层职责

1. `cn.vastplan.platform.data.relational.connection-manager` 是管理面插件，继续独占连接定义、CredentialRef、变更版本和用户管理 API；它不持有明文、不打开物理连接、不维护连接池。
2. 新增第一方基础插件 `cn.vastplan.foundation.data.relational.runtime` 作为数据库数据面。它以受信任的 dedicated 进程运行，拥有 Provider 注册表、本地连接池、查询/事务执行、健康和指标。它可以 active-active 多副本部署。
3. Backend Kernel 只保留通用 capability 路由、调用上下文、Credential lease 授权和进程生命周期，不保存数据库驱动对象或池。现有 `kernelspi.DatabaseBroker` 在迁移期只作为可信宿主窄适配面，稳定 wire 契约完成后由 Database Runtime capability 取代。

Database Runtime 不与无关插件共享语言 Host。原因不是性能，而是它会在极短时间内持有解封后的凭证和数据库会话；共享地址空间会把同池插件扩大到秘密信任边界。

### 2. Provider 契约

Provider 使用稳定、与驱动库无关的内部 SPI，至少提供：

- Provider ID、版本、能力与配置 JSON Schema；
- `Validate`、`OpenPool`、`Probe`、`ClosePool`；
- 参数绑定、分页/流式读取、事务和错误分类能力；
- TLS、只读、隔离级别等支持矩阵。

首批必须同时实现 `postgresql` 和 `mysql`，以代码验证多 Provider 边界；`psql` 是 PostgreSQL 客户端程序名，不作为 Provider ID。SQL Server、Oracle 和其他关系数据库通过相同 SPI 后续接入。

第一方 Provider 初期随 Database Runtime 签名制品构建和注册，以获得稳定 ABI 与较低调用开销。未来第三方 Provider 只能经独立受限进程和版本化 RPC 接口接入，不使用 Go `plugin` ABI，也不能取得其他连接的凭证或池对象。

### 3. 连接池与资源预算

每个 Database Runtime 实例只维护自己的本地池，不把 socket 或驱动对象跨进程、跨节点共享。Pool key 至少包含：

`tenant + project/workspace + connection resource ID + provider ID/version + non-secret config digest + credential version`

每个池支持 `minIdle`、`maxOpen`、`maxIdle`、`maxLifetime`、`maxIdleTime`、`acquireTimeout`、健康周期和空闲池 TTL。连接定义给出期望值，部署级安全上限和节点总预算具有最终裁剪权；插件不能通过配置突破宿主上限。

集群总连接预算必须按最坏重叠计算：

`数据库允许连接数 >= 单实例 maxOpen × 最大副本数 × 轮换重叠代数 + 运维保留量`

默认轮换重叠代数为 2。若预算不足，发布在创建新池前失败，不通过静默多开连接“尽力而为”。连接获取必须受租户和调用方并发/队列上限约束，避免单个插件耗尽全节点连接。

### 4. 凭证消费

现有 Material Lease 只授权 Kernel `SYSTEM` caller 解封。独立 Database Runtime 接入时扩展为“可信运行实例 audience”，但不能放宽为普通插件：

1. Kernel 在启动 dedicated Database Runtime 时签发短期、单实例、绑定插件 ID/发布者/制品摘要/节点/目的的启动身份；Runtime 生成一次性 X25519 接收密钥。
2. 凭证访问策略只允许精确的第一方 Database Runtime 身份请求其当前操作所需 owner、purpose 和 CredentialRef；凭证插件仍对 Active 状态做二次确认并把租约密封到该实例公钥。
3. Runtime 只在创建或轮换物理连接时本地解封，明文不返回 Kernel、业务插件或管理面。解封缓冲区在同步使用后尽力清零。
4. 不允许把租约密文、解封密钥或数据库口令放入连接池 key、状态快照、日志、指标、追踪或事务句柄。

在可信运行实例身份和访问策略完成前，生产 Database Runtime 必须 fail-closed；不得临时改为环境变量明文或让普通插件调用 material lease。

### 5. 集群与事务亲和

普通无状态、幂等调用可由 capability Router 在健康 Database Runtime 副本间负载均衡。连接池不要求副本间一致，只需相同连接定义 revision 和凭证版本最终收敛。

事务开始后返回短期、签名且不透明的 transaction handle，至少绑定 tenant、调用方、connection revision、Runtime instance ID、opaque transaction ID 和 expiry。后续调用必须路由回同一实例，不能在另一实例“恢复”驱动事务对象。

Runtime 实例崩溃时，数据库连接断开并由数据库回滚；调用方收到稳定的 `TRANSACTION_LOST`，只能从整个事务边界按幂等策略重试。事务 handle 不含 SQL、凭证、连接地址或可用于伪造亲和的信息。

### 6. 配置、轮换与升级

Provider 配置必须先通过其 JSON Schema 和语义校验；禁止用包含密码的任意 DSN 绕过字段级约束。TLS 默认安全开启，关闭验证必须经过显式部署策略和审计。

连接定义或 CredentialRef 版本变化时，Runtime 创建新 generation 池并完成 probe，原子切换新请求，再 drain 旧池；旧事务可在有界窗口结束，超时后回滚关闭。Provider/Runtime 升级同样采用 warm、switch、drain，不原地替换正在使用的驱动对象。

### 7. 可观测与故障处理

按 Provider、connection resource、Runtime instance 暴露池总数、open/idle/in-use、等待数/时长、超时、健康、轮换 generation 和事务数；tenant/connection 标识须散列或受权限保护。指标和日志绝不包含 endpoint 完整串、SQL 参数、DSN、CredentialRef handle 或 material。

Provider panic/崩溃由 ADR-0094 的 Guardian 和协议心跳收敛。第三方 Provider 进程崩溃只能影响其 Provider 池；第一方同进程 Provider 故障会重启该 Database Runtime 实例，因此仍需副本和事务丢失语义。

## 实施顺序

1. 固化 Database Runtime wire API、错误码、事务 handle 和 Provider SPI；实现内存 fake Provider 契约测试。
2. 完成可信运行实例 identity、Material Lease audience 与最小访问策略。
3. 实现连接池管理器、资源预算、generation 轮换与指标。
4. 同批实现 PostgreSQL 和 MySQL Provider，并运行真实数据库集成测试。
5. 接入 connection-manager 的 probe/发布闭环和 active-active 服务组合。
6. 完成事务亲和、实例故障回滚、凭证轮换和连接预算故障注入门禁。

## 备选方案

- **池管理放进 Kernel**：调用链最短，但把具体数据库生命周期、驱动依赖和秘密暴露面带入微内核，否决。
- **每个数据库一种独立管理插件**：实现直接，但重复连接定义、凭证、池、事务和治理逻辑，否决；差异只留在 Provider。
- **普通业务插件各自建池**：局部灵活，却无法统一连接预算、轮换和审计，并扩大凭证面，否决。
- **全平台唯一中央数据库网关**：管理集中，但形成吞吐和故障瓶颈。Database Runtime 可按服务部署并集群化，不强制单点。
- **每次调用新建连接**：没有常驻池，但延迟、数据库认证压力和资源抖动不可接受，否决。

## 当前实现状态与影响

- 已有连接管理插件、托管凭证 Saga、Kernel Material Lease 适配器、真实 Provider、连接池、持久化 publication outbox、active-active 无状态执行、实例亲和事务和 PostgreSQL/MySQL 故障注入门禁；事务与池的标准指标出口尚未完成。
- 新方案保持 Kernel 不感知 PostgreSQL/MySQL 驱动，新增数据库类型不要求修改或重启 Kernel 二进制，只需发布兼容的 Database Runtime/Provider 制品。
- dedicated 可信进程比内嵌驱动多一次本机协议调用，但换来凭证隔离、独立升级、故障恢复和集群扩缩容；数据库网络时延通常远高于这层本机调用成本。

## 实施进展（2026-07-20）

实施顺序第 1 项已完成：新增 `contracts/schemas/database/v1` 机器可执行 JSON wire 契约、显式无损值类型、稳定 `database.runtime.*` 错误码、事务句柄格式和严格语义校验；`ManagedCredentialRef` 提升到 common/v1 单一类型，`pluginconfig` 保留兼容别名。

基础插件 `cn.vastplan.foundation.data.relational.runtime` 已建立第一方 Provider/Pool/Transaction SPI、冻结式 Registry、MaterialSource 边界和 fake PostgreSQL/MySQL 契约测试。0.1.0 只在 descriptor 中开放无敏感性的 `providers` 操作；其余操作虽已固化 wire 契约，但在实施顺序第 2 项可信运行实例 identity 完成前继续 fail-closed。

## 实施进展（2026-07-20，可信 Runtime material）

实施顺序第 2 项已完成，Database Runtime 制品升级为 0.2.0。Backend Host 将验签后的插件 ID、发布者、版本、制品 SHA-256、节点、service unit 和随机启动实例保存在 host-only identity 中，以摘要形成 Material Lease audience；身份不接受插件 payload，也不签发可重放 bearer token。

新增 `kernel.credential.material-lease` 窄中继与 Runtime 内部 `MaterialSource`：Runtime 持有一次性 X25519 私钥，Kernel 只转发 CredentialRef、公钥和密文，不接触明文。策略精确限制为首方 Database Runtime 与 connection-manager 的 `database.connection` 引用，并覆盖伪造 caller、第三方发布者、跨 owner、跨 tenant、错 audience、过期 lease 和回调后清零测试。`providers` 仍是唯一公开操作；下一步进入连接池管理器、资源预算、generation 轮换和指标。

## 实施进展（2026-07-20，Pool Manager A1）

实施顺序第 3 项已完成，Database Runtime 制品升级为 0.3.0。统一 Pool Manager 在打开候选池和获取 material 前执行节点、租户、连接与重叠 generation 预算检查；连接定义 revision 或凭证版本变化时先打开并 probe 候选池，再原子切换新请求，旧池只接收在途 lease 并在有界窗口内 drain。

调用入口同时受每池等待队列、每调用方并发和 `acquireTimeout` 限制。关闭失败的 generation 保持 `draining` 并继续占用预算，后续关闭流程可重试；关闭历史采用全局有界保留，避免大量已删除连接造成内存增长。快照只暴露摘要化 scope/connection、Provider、revision、generation、预算、等待、在途和健康统计，不输出 endpoint、CredentialRef 或 material。fake Provider 已覆盖幂等激活、并发轮换、预算前置拒绝、队列过载、强制排空、关闭失败重试和历史上限，并通过竞态检测。下一步进入实施顺序第 4 项：同批接入 PostgreSQL 与 MySQL Provider 及真实数据库集成测试。

## 实施进展（2026-07-20，真实 Provider A2）

实施顺序第 4 项已完成，Database Runtime 制品升级为 0.4.0。制品同批注册 pgx 5.10.0 PostgreSQL Provider 与 go-sql-driver/mysql 1.10.0 MySQL Provider，两者复用 `database/sql` 池适配、wire 值转换、结果截断、事务隔离/超时和稳定错误分类。Provider 只接受各自签名 JSON Schema 中的非敏感字段，不接受 DSN；默认 `verify-full` 和系统信任根，关闭 TLS 必须由宿主部署策略显式放行。PostgreSQL 禁止 `PGSERVICE/PGSERVICEFILE` 注入并使用受控配置模板，MySQL 禁用明文回退、旧认证、任意本地文件与多语句。

每条物理连接通过 `MaterialSource` 单独取得 material，长期 `database/sql.DB` 不保存密码 DSN，认证后清空 Runtime 自己持有的候选配置。pgx、go-sql-driver/mysql 和 Go immutable string 均无法承诺驱动私有连接副本中的认证字符串可原地擦除，因此该风险不再被文档隐藏，而由 dedicated 可信进程、短时 lease、最小内存暴露、关闭物理连接和 generation drain 控制。单元契约覆盖两个 Provider 的注册、TLS fail-closed、严格配置、minIdle 预热、无损值类型、结果上限、事务和错误分类；真实数据库门禁通过显式测试环境变量启用，本次已在 PostgreSQL 17.10 与 MySQL 8.0.42 临时实例上完成 probe 和参数化查询验收。下一步进入实施顺序第 5 项。

## 实施进展（2026-07-20，发布与 active-active A3）

实施顺序第 5 项已完成，Database Runtime 升级为 0.5.0，connection-manager 升级为 0.4.0。连接定义状态格式 v3 新增稳定的 96-bit 摘要 `resourceId`、单调 revision、删除后保留的非秘密 revision tombstone、Provider options、PoolPolicy 和持久化 publication outbox。凭证 candidate 激活后才形成期望定义；Runtime `activate/retire` 成功后才完成 publication，并按顺序推进旧 CredentialRef 退役。管理面列表明确报告 `ready/pending`，其中 ready 只表示至少一个 queue 副本已接受该 revision，不冒充全副本预热。

active-active 不采用不可验证的“重复 queue 调用即广播”。管理面主动发布命中一个副本；其他副本在首次接到该 revision 的 `query/execute` 时，通过只允许精确 Runtime caller 的内部 `platform.database/resolveRuntime` 读取该单条现行定义，随后幂等建池。Runtime 的成功定义确认使用 1 秒有界 lease，同一连接的并发过期验证在副本内合并；删除或 revision 失效会排空该 tenant 在本副本内所有 project 池，管理面不可达且 lease 过期时 fail-closed。这样每个活跃连接每副本最多增加约 1 次/秒轻量管理面校验，同时把未命中主动 retire 的 stale-serving 窗口限制为 1 秒。已删除定义、旧 revision、用户直调和缺少 `database.connection/<resourceId>` 宿主投影 grant 的执行请求均 fail-closed。平台本地组合新增两个 Database Runtime 副本，使用 `external-shared + cluster + queue`。

`probe` 已从临时 `kernel.database.probe` 迁移到 Database Runtime Material Lease 数据面；Backend Kernel 不再承担数据库 Broker 路径。`providers/probe/activate/retire/query/execute` 已进入签名 descriptor，事务操作继续关闭。下一步进入实施顺序第 6 项。

## 实施进展（2026-07-20，实例亲和事务 A4）

实施顺序第 6 项的事务主链已完成，Database Runtime 升级为 0.6.0。通用 `CallTarget` 新增可选 `instance_id`，addressing registration 同时订阅共享 queue subject 与实例独立直达 subject；Router 只对同 logical service/routing domain/partition 范围内存在健康租约的精确实例发流量，未知、draining 或已撤销实例 fail-closed。Node Agent 使用宿主签发的非秘密 Runtime audience 作为非分片能力实例路由身份，分片注册追加 partition 后缀避免目录键冲突。该能力是短期状态亲和的通用路由原语，不改变 active-active 默认均衡，也不替代 partition fencing。

`begin` 取得当前 pool generation lease 和驱动事务，返回 `vptx1` 句柄。完整声明由每次进程启动随机生成的 AES-256-GCM 密钥加密认证，绑定 tenant、project、caller、ConnectionRef、owner Runtime audience、随机 transaction ID 和 expiry；外部只看到非秘密 owner 路由前缀，不包含 SQL、参数、endpoint、CredentialRef 或 material。后续请求仍可进入任意 queue 副本，非 owner 在校验原调用主体及连接 grant 后通过未公开、仅允许精确 Runtime caller 的 `transactionRelay` 直达 owner；owner 再验证加密句柄与原 scope/caller。查询、执行和提交必须重新持有当前连接 grant，回滚只要求原 caller/scope，以便授权撤销后仍能安全清理。owner 租约消失、进程重启密钥变化或本地事务丢失统一返回可重试 `database.runtime.transaction_lost`，过期自动回滚并稳定返回不可重试 `database.runtime.transaction_expired`。

事务占用 generation lease，连接/凭证 revision 轮换后可在既定 drain 窗口内完成；强制 drain 关闭旧 generation 时会唤醒事务注册表、回滚并释放调用方/池槽位。每个 Runtime 默认最多 4096 个活动事务，service 级 `maxTransactions` 施加硬上限。契约、caller/scope/连接绑定、句柄篡改、跨副本 relay、owner 离线、超时回滚、轮换有界 drain 和精确实例寻址均有自动化测试。真实数据库故障门禁见下一节；完成后只剩标准指标出口。

## 实施进展（2026-07-20，真实数据库故障矩阵 A5）

实施顺序第 6 项的真实数据库故障门禁已完成。新增显式入口 `engineering/tools/database-fault-matrix.sh`，以只绑定 `127.0.0.1` 固定临时端口的 PostgreSQL 17.10 与 MySQL 8.0.42 容器运行相同语义矩阵；日常单测没有 `_FAULT_CONTAINER` 时跳过，禁止误操作开发者已有数据库。脚本负责启动、就绪等待、总超时和失败回收，镜像可覆盖以扩展版本矩阵。

两个 Provider 均已实际验证：反向加锁产生的数据库死锁映射为可重试 `database.runtime.transaction_conflict`；调用方并发/池预算耗尽映射为可重试 `database.runtime.pool_exhausted` 且计数收敛；连接 revision 切换后旧事务在 generation drain 上界被回滚并稳定返回可重试 `database.runtime.transaction_lost`；容器网络冻结映射为可重试 deadline，解冻后原池重新通过 probe；数据库停止期间映射为可重试 connection unavailable/deadline，使用同一 endpoint 重启后原池恢复。A5 不引入长期测试数据库、明文 DSN 或生产 TLS 放宽。剩余工作收敛为 A6：标准指标出口、事务计数和告警验收。
