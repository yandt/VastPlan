# 平台控制数据库 Bootstrap

本文是 `PlatformControlStoreProfile`、Bootstrap Secret Provider、两阶段状态机和 `state.shared.v1` 运行期绑定的实现真源。数据分层决策见 [ADR-0194](../decisions/ADR-0194-平台控制数据库与声明式数据分层.md)，恢复分级见 [ADR-0169](../decisions/ADR-0169-Seed-Recovery-Capsule与分阶段可用性.md)。

## 1. 本机只保存非敏感根

`contracts/schemas/platformcontrol/v1` 定义严格 Profile：generation、PostgreSQL/MySQL Provider、`host:port`、逻辑 database/schema、TLS、username、secretRef 和 Database Runtime capability range。Profile 不允许 DSN、URL、密码或任意驱动参数。

Profile 文件必须使用规范绝对路径、普通文件和 owner-only 权限。提交采用 `expected generation -> candidate generation=expected+1` 的 CAS、临时文件、fsync 和原子 rename；初始化成功但 Profile CAS 失败时不得绑定候选 Store，调用方可按同一幂等初始化流程重试。

## 2. 秘密只在回调期间存在

组合根根据 Profile 的 `secretRef` 选择一次 `SecretSource`：

- Linux 生产使用 `systemd-credential`，只允许安全 credential name，并从规范 `CREDENTIALS_DIRECTORY` 读取；
- 开发使用 `owner-file`，要求规范绝对路径、普通文件和无 group/world 权限；
- Vault/KMS 后续通过同一 `SecretSource` 接口增加，不复制 Bootstrap 流程。

首次配置还允许浏览器提交一次性的 `secretMaterial`。它属于 `ChangeRequest` 而不是 Profile：固定 BFF 完成会话、角色、CSRF、同源和请求体上限校验，共享 `platformcontrol/v1` 校验器在插件与可信宿主两级执行精确字段、互斥秘密输入和 64 KiB 材料上限校验；第一方 Connection Manager 只经固定 Kernel Service 转发。Go 可信宿主把材料写入自身受控 `managed-secrets` 目录中的随机命名 `0600` 普通文件，再把标准 `owner-file` 引用写入 Profile。测试请求只创建候选文件并在结束后删除；配置请求只有数据库 Test/Initialize 成功后才原子提交文件，Profile CAS 失败会回滚文件。启动时会按当前 Profile 清理未引用候选和孤儿文件。

候选密码跨独立 Database Runtime 进程传递时，Wire Profile 在 Test/Initialize 阶段必须引用当前真实存在的临时 `0600` 文件，不能提前引用尚未创建的最终文件名。Initialize 成功后可信宿主先原子重命名密码文件，再把 Profile 中的 `secretRef` 切换为最终路径并执行 Profile CAS；失败或仅测试时同时删除临时与最终候选。Runtime 始终只按受限引用重新读取密码，协议中不传递密码明文。

最小 Bootstrap 页面与 Portal Edge 通过 `X-VastPlan-Bootstrap-Page-Contract` 执行轻量契约握手，当前页面契约为 `5`。测试或初始化前发现已打开页面落后于当前宿主时，页面必须自动刷新，禁止把旧表单结构送入新后端后再显示泛化 Schema 错误。响应继续使用 `Cache-Control: no-store`，但不能依赖缓存策略替换已经运行在浏览器中的旧 JavaScript。

`Platform Control ready` 只表示 SQL Shared State 已提交并绑定，不表示完整平台组合或 Portal Activation 已经就绪。配置页在数据库 Ready 后必须继续以有界退避探测规范 `/v1/portal-runtime`；只有目标路径存在当前 Activation 时才进入 `/operations`。Portal Host 对直接访问 `/operations` 也执行同一判断：未配置数据库时重定向到 Bootstrap，数据库已就绪但 Portal 尚未发布或平台仍在收敛时继续呈现最小 Bootstrap 状态页，不能先加载 SPA 再让浏览器以 `RUNTIME_FETCH_FAILED` 失败。

页面提交必须先从仍处于启用状态的控件生成一次性请求快照，再禁用表单并取得 CSRF Token。禁止在 `disabled` 之后通过 `FormData` 读取表单，因为浏览器不会提交 disabled 控件，这会把完整配置静默退化为空候选。

首次配置时，最小 Bootstrap 页面与 Connection Manager Workbench 页面统一预填逻辑数据库 `vastplan`。PostgreSQL 显示并预填专用 Schema `platform`，物理表按 `"platform"."table_name"` 限定；MySQL 的 schema 与 database 是同一命名空间，页面不显示独立 Schema，可信请求构造层强制令 `schema=database`，物理表按 `` `database`.`table_name` `` 限定。默认值只是可编辑的输入初值，不是内核硬编码；提交后仍以签名 Profile 中的实际值为准。

运行中的服务不能反写 systemd 创建的 `$CREDENTIALS_DIRECTORY`。`systemd-credential` 始终表示部署环境已注入的外部引用；未来若支持页面安装 systemd Credential，必须由部署控制器生成 `LoadCredentialEncrypted` 制品、修改 unit 并受控重启，不能伪装成本地文件写入。

秘密最大 64 KiB，只借给同步回调，回调结束立即清零缓冲区。Profile、状态、日志和错误码不包含秘密或原始数据库错误。

## 3. 两阶段状态机

可信 Controller 只依赖 `ProfileStore`、`SecretResolver`、`SecretMaterialStore`、`Bootstrapper` 和 `BindingStore` 五个端口。`Bootstrapper`、`ManagedStore` 与 `SecretSource` 放在中立 `extensions/libraries/go/platformcontrol`，文件材料 Store 由核心可信宿主拥有，避免 Database Runtime 插件反向链接内核实现：

```text
Start
  ├─ 无 Profile → unconfigured
  └─ 有 Profile → Open → Bind → ready

Configure
  Test
    ├─ 目标库存在 → Initialize → Profile CAS Commit → Bind → ready
    └─ 首次配置 + 显式允许创建 + database_not_found
         → Provision → Test → Initialize → Profile CAS Commit → Bind → ready
```

状态只有 `unconfigured / testing / provisioning / initializing / ready / recovery` 和稳定错误码。`TestCandidate` 始终只读；首次配置已允许自动建库时，Runtime 返回 `database_not_found` 表示服务器连接检查已到达建库前置边界，页面按“连接测试成功”呈现，但仍不创建数据库。只有 `Configure` 可以触发 Provision，且 `createDatabaseIfMissing` 只允许 generation 0。启动 `Open`、恢复、普通数据库连接和已有 Platform Control Profile 更新均不会创建或删除逻辑数据库。尚无已提交 Profile 时，Shared State 返回稳定的 `state.unconfigured`；首次候选在提交前失败仍保持该状态，使 Seed 登录可以继续进入数据库配置页。已有 Ready generation 的替换候选失败时保留旧 Store 和旧 generation，只投影候选错误码。Profile 已提交但绑定异常属于不应发生的信任边界故障，必须 recovery，不能继续把旧 Store 冒充当前 Profile。

数据库候选失败沿单一诊断协议返回。Database Runtime 是分类真源，负责把 Provider/驱动错误归一为稳定、无敏感值的 `database.runtime.*`；Remote Bootstrapper、Controller 和 Backend Kernel 只能保留该类别及 retryable 属性，不能再次压缩成通用 `database_unavailable`。Portal BFF 把类别映射为本地化公开码，并把本次调用的 32 位十六进制 `traceId` 返回页面；Bootstrap 内部跳转沿可信上下文继承同一 trace，因此页面编号能够直接关联 Runtime 日志。原始驱动消息只留在 Runtime 进程内，日志仅记录 SQLSTATE/MySQL 错误号或 DNS、网络、TLS 等脱敏诊断。

## 4. 不可回退 Shared State 绑定

`sharedstate.BindingStore` 是 Kernel 暴露给插件的稳定 `state.shared.v1` 端口。它以单调协议状态区分 `unconfigured` 与 `unavailable`：只有从未提交 Platform Control Profile 时返回前者；读取到既有 Profile 或首次 Profile CAS 提交成功后，Provider 即被永久标记为必需，此后尚未绑定或发生故障一律返回后者。它只接受更高 generation 的 Provider，同 generation 只有相同 Profile identity 才幂等。Provider 故障不会切回本地 JSON，避免同一插件出现 SQL 与文件双真相源。候选 Store 在 Profile 提交或绑定失败时必须关闭自身连接池，不能留下不可达 generation 的数据库连接。

Authentication Broker 只把 `state.unconfigured` 解释为“尚未建立平台控制库”，此时可读取只读 Seed Catalog 完成首次登录；`state.unavailable`、超时、损坏和未知错误仍全部 fail-closed。该判断由 Shared State 协议和 Broker 存储适配器完成，前端、工作流和各层 Loader 不传递开发/生产开关。

File Provider 仍只服务单测、开发未初始化阶段和明确的 Seed/Recovery 根状态，不能绑定为生产 Platform Control Store。

Database Runtime 内部的 `platformcontrolbootstrap` 适配器复用 PostgreSQL/MySQL Provider Registry 与本地连接池，不把密码、驱动或连接池交给普通服务。它负责：

- 使用受控 `SecretSource` 建立候选连接池；
- 首次显式 Provision 使用最大 1 个连接的短生命周期管理池：PostgreSQL 连接标准 `postgres` 维护库，MySQL 连接服务器但不选择默认 database；目标名称先按 Provider 标识符长度校验，再以方言安全引用生成 `CREATE DATABASE`；
- 对 PostgreSQL 创建并使用 Profile 指定 schema；MySQL 首期要求 schema 等于逻辑 database；
- 在 pinned session 内取得数据库迁移锁，建立限定 schema 的 Shared State 表；
- 初始化后重新打开并执行健康探针，只有成功的 `ManagedStore` 才允许绑定；
- 支持 `verify-full / verify-ca / disable`，其中 `verify-ca` 校验证书链但不校验主机名。

该适配器是 Database Runtime 内部模块，不是新插件，也不允许 Backend Kernel 链接具体数据库 Provider。Bootstrap Tier 生命周期只调用该统一端口。

## 5. 当前状态

P4a 已完成 Profile 契约、owner-only CAS 文件存储、systemd/development file Secret Source、两阶段 Controller 和不可回退 Binding Store，并覆盖空环境、首次成功、失败不提交、旧代保留、权限与秘密清零测试。

P4b 已完成 Database Runtime 进程内 Bootstrap 适配：PostgreSQL/MySQL 初始化、限定 schema SQL Shared State、迁移锁、候选连接池回收和 TLS `verify-ca`。公开 Database Runtime Capability 为 `1.3.0`。

P4c 已把进程边界接通：

- Database Runtime `0.16.2` 同时贡献公开数据面、`foundation.data.record-store@1.2.0`，以及仅宿主可调用的 `foundation.state.shared.sql.bootstrap@3.0.0` 和 `foundation.state.shared.sql@1.0.0`；Bootstrap 3.0 使用与普通连接相同的 `DatabaseConnectionCandidate`，并新增首次显式建库操作，但仍由可信宿主管理 secret、Profile 和 Store 绑定；Controller 从 Deployment 锁定的已验证制品投影 DataModel Inventory，只向该 Runtime 注入宿主保留配置，Node Agent 在候选能力进入公开路由前使用 Host 固定的 SYSTEM 身份同步，并在收到与候选目录精确绑定的 Schema Activation 授权后执行迁移，再从普通插件配置中摘除目录；其用户配置 `clusterMaxOpen` 与 Scheduler 可信派生的 `clusterMaxReplicas` 在组合根生成每实例连接硬预算，覆盖 active-active 与双代轮换的最坏连接占用；
- Bootstrap Capability 只接受固定 SYSTEM caller `platform-control-bootstrap/primary`，目标 logical service 和 routing domain 也由宿主固定；
- 宿主用 `RemoteBootstrapper/RemoteStore` 把跨进程 Capability 重新适配为原有 `sharedstate.Store`，业务插件看不到传输差异；
- Deployment/Assignment 增加 `startup_tier=bootstrap|full`，默认 `full`。Node Agent 可以完成 Full 单元的下载、验签和安装，但在 Shared State Ready 前拒绝激活；
- 恢复由已验证能力目录的 topology change 触发，没有定时兜底轮询；Runtime 重建并重新登记 Capability 后，Controller 按已提交 Profile 幂等执行 Open；
- P6 已完成多副本逐实例初始化：可信宿主从已验证能力目录枚举精确 Runtime Instance，按同节点优先、稳定 Instance ID 的顺序选择首个副本执行一次 `Initialize`，其余副本只执行幂等 `Open`。全部副本打开成功后才原子替换可调用集合；任一新副本失败时保留上一组已打开副本，绝不把半初始化实例暴露给 Shared State。
- `RemoteStore` 对每次调用重新结合当前可信目录与已打开集合，并固定 `CallTarget.instance_id`。只有传输失败才依次切换远端副本；CAS 冲突、权限、参数和其他应用错误不会跨副本重放。目录 topology change 会更新共享副本集合，既有 Store 无需 Kernel 重新绑定即可看到新增、移除和本地优先顺序。
- 普通 Record Store 调用沿同一能力目录本地优先；事务仍使用加密 Transaction Handle 固定 owner Runtime，非 owner 通过受限 relay 精确路由，owner 丢失稳定返回 `transaction_lost`。副本各自维护本地连接池，不共享 socket。

P4d 已完成受限管理闭环：

- Controller 实现统一 `Administration` 端口，`Status / TestCandidate / Configure` 均沿同一 Profile、SecretResolver 和 Bootstrapper 调用链执行；测试候选不会初始化数据库、提交 Profile 或绑定 Store；
- Backend Kernel 只向精确的 Connection Manager 插件开放三个 `kernel.platform-control.*` 服务，插件策略与 Seed Profile 的 kernel service grant 同时收紧；
- Connection Manager 公开 `platformControlStatus / platformControlTest / platformControlConfigure`，但自身不解释数据库驱动和秘密；其 Bootstrap 依赖于 Credentials 的部分降级为 soft，普通连接管理工作流在 Credentials 未就绪时仍然 fail-closed；
- Portal BFF 使用固定、CSRF 保护且受 Management Binding 和角色权限约束的 `/platform-control` 路由。页面默认接收一次性密码材料，也可选择已有 systemd credential 或 owner-only 文件引用；密码不进入 Profile、响应、日志、错误详情、浏览器持久存储或热更新胶囊；
- 同一数据库全栈插件增加 Workbench Form Page，覆盖未配置、测试、建库、初始化、Ready 和 Recovery 状态。当前非敏感 Profile 可在授权页面回填，语言切换仍由插件目录驱动。

P4e 已完成开发编排器与最小 Portal 的两阶段闭环：

- `platformdev` 总是为 Backend Kernel 注入受保护的 Profile 与 credential directory，并只等待 Recovery Tier 4 个 Bootstrap 单元后对外宣告最小服务就绪；ControlPlane/Platform Tier 在 SQL Shared State 绑定后由同一 Desired Revision 继续收敛，人工首次配置没有虚假的启动超时；
- Recovery Capsule 按每个单元实际应用的 revision 评估分阶段就绪，较高 Tier 尚未应用不会反向把 Recovery Tier 判为失败；
- Node Portal Host 提供固定 `/bootstrap/platform-control` 最小界面和 `/v1/bootstrap/platform-control` BFF，只能在已认证 Seed 会话、数据库角色和 CSRF 边界内访问固定 `platform.database` logical service。它不加载普通 Portal Generation，不允许浏览器选择 capability、服务或路由；
- 该 Host 页面是数据库全栈能力的恢复安全呈现面，不是新的应用插件。完整平台 Ready 后仍由 Connection Manager 的 Workbench 页面承担日常管理；
- Shared State 绑定成功会发出一次有界组合触发，Node Agent 复用同一 Planner/Activation 数据链继续激活 Full 单元，不增加状态轮询；
- Go 能力目录 `schema_version=2` 的 ArtifactIdentity、ContractIdentity 与 fingerprint 已同步到 Node Addressing SDK，Portal 不再静默丢弃新目录记录。

真实进程空环境验收已覆盖 Recovery `4/4 Ready`、完整 Tier 保持 Pending、最小页面可达和固定 Bootstrap API 可路由；三副本契约测试覆盖同节点优先、两级传输故障转移、应用错误不重放、拓扑增删及新副本打开失败保留旧集合。PostgreSQL/MySQL 的真实初始化、重启保留和多节点迁移锁竞争仍由集成矩阵完成，不能仅凭内存协议测试宣称数据库故障矩阵已经封板。

P5 第一批迁移已删除 Connection Manager 的直接 JSON 状态文件。普通数据库管理请求通过统一 Workflow 进入按租户、Leader-fenced 的 Shared State CAS 聚合；连接定义、凭证候选、Runtime publication outbox 和回收队列保持原有原子边界。Platform Control 配置操作在 Shared State 尚未绑定时仍走独立受限内核端口，因此不会形成“为了配置数据库而先依赖数据库”的循环。
