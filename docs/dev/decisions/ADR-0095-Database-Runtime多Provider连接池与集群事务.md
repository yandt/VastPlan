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

- 已有连接管理插件、托管凭证 Saga、Kernel Material Lease 适配器和多服务 capability 路由；Database Runtime、真实 Provider 和连接池尚未实现。
- 新方案保持 Kernel 不感知 PostgreSQL/MySQL 驱动，新增数据库类型不要求修改或重启 Kernel 二进制，只需发布兼容的 Database Runtime/Provider 制品。
- dedicated 可信进程比内嵌驱动多一次本机协议调用，但换来凭证隔离、独立升级、故障恢复和集群扩缩容；数据库网络时延通常远高于这层本机调用成本。
