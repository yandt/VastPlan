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

秘密最大 64 KiB，只借给同步回调，回调结束立即清零缓冲区。Profile、状态、日志和错误码不包含秘密或原始数据库错误。

## 3. 两阶段状态机

可信 Controller 只依赖 `ProfileStore`、`SecretResolver`、`Bootstrapper` 和 `BindingStore` 四个端口。`Bootstrapper`、`ManagedStore` 与 `SecretSource` 放在中立 `extensions/libraries/go/platformcontrol`，避免 Database Runtime 插件反向链接内核实现：

```text
Start
  ├─ 无 Profile → unconfigured
  └─ 有 Profile → Open → Bind → ready

Configure
  Test → Initialize → Profile CAS Commit → Bind → ready
```

状态只有 `unconfigured / testing / initializing / ready / recovery` 和稳定错误码。首次配置任一步失败都进入最小 recovery 且 Shared State 保持 unavailable；已有 Ready generation 的替换候选失败时保留旧 Store 和旧 generation，只投影候选错误码。Profile 已提交但绑定异常属于不应发生的信任边界故障，必须 recovery，不能继续把旧 Store 冒充当前 Profile。

## 4. 不可回退 Shared State 绑定

`sharedstate.BindingStore` 是 Kernel 暴露给插件的稳定 `state.shared.v1` 端口。它启动时 unavailable，只接受更高 generation 的 Provider；同 generation 只有相同 Profile identity 才幂等。Provider 故障不会切回本地 JSON，避免同一插件出现 SQL 与文件双真相源。候选 Store 在 Profile 提交或绑定失败时必须关闭自身连接池，不能留下不可达 generation 的数据库连接。

File Provider 仍只服务单测、开发未初始化阶段和明确的 Seed/Recovery 根状态，不能绑定为生产 Platform Control Store。

Database Runtime 内部的 `platformcontrolbootstrap` 适配器复用 PostgreSQL/MySQL Provider Registry 与本地连接池，不把密码、驱动或连接池交给普通服务。它负责：

- 使用受控 `SecretSource` 建立候选连接池；
- 对 PostgreSQL 创建并使用 Profile 指定 schema；MySQL 首期要求 schema 等于逻辑 database；
- 在 pinned session 内取得数据库迁移锁，建立限定 schema 的 Shared State 表；
- 初始化后重新打开并执行健康探针，只有成功的 `ManagedStore` 才允许绑定；
- 支持 `verify-full / verify-ca / disable`，其中 `verify-ca` 校验证书链但不校验主机名。

该适配器是 Database Runtime 内部模块，不是新插件，也不允许 Backend Kernel 链接具体数据库 Provider。P4 后续检查点只需通过 Bootstrap Tier 生命周期调用该统一端口。

## 5. 当前状态

P4a 已完成 Profile 契约、owner-only CAS 文件存储、systemd/development file Secret Source、两阶段 Controller 和不可回退 Binding Store，并覆盖空环境、首次成功、失败不提交、旧代保留、权限与秘密清零测试。

P4b 已完成 Database Runtime 进程内 Bootstrap 适配：PostgreSQL/MySQL 初始化、限定 schema SQL Shared State、迁移锁、候选连接池回收和 TLS `verify-ca`。公开 Database Runtime Capability 提升至 `1.2.0`，插件提升至 `0.9.0`。Bootstrap Tier 启动、配置 API/UI 和恢复动作仍属于 P4 后续。

P4c 已把进程边界接通：

- Database Runtime `0.11.0` 同时贡献公开数据面、仅宿主可调用的 `foundation.state.shared.sql.bootstrap@1.0.0` 和 `foundation.state.shared.sql@1.0.0`；其 `clusterMaxOpen/maxReplicas` 在组合根派生每实例连接硬预算，覆盖 active-active 与双代轮换的最坏连接占用；
- Bootstrap Capability 只接受固定 SYSTEM caller `platform-control-bootstrap/primary`，目标 logical service 和 routing domain 也由宿主固定；
- 宿主用 `RemoteBootstrapper/RemoteStore` 把跨进程 Capability 重新适配为原有 `sharedstate.Store`，业务插件看不到传输差异；
- Deployment/Assignment 增加 `startup_tier=bootstrap|full`，默认 `full`。Node Agent 可以完成 Full 单元的下载、验签和安装，但在 Shared State Ready 前拒绝激活；
- 恢复由已验证能力目录的 topology change 触发，没有定时兜底轮询；Runtime 重建并重新登记 Capability 后，Controller 按已提交 Profile 幂等执行 Open；
- P6 完成多副本逐实例初始化和事务亲和前，种子 Database Runtime 固定为单副本，禁止把 queue 路由误当成多副本 Shared State 已就绪。

P4d 已完成受限管理闭环：

- Controller 实现统一 `Administration` 端口，`Status / TestCandidate / Configure` 均沿同一 Profile、SecretResolver 和 Bootstrapper 调用链执行；测试候选不会初始化数据库、提交 Profile 或绑定 Store；
- Backend Kernel 只向精确的 Connection Manager 插件开放三个 `kernel.platform-control.*` 服务，插件策略与 Seed Profile 的 kernel service grant 同时收紧；
- Connection Manager 公开 `platformControlStatus / platformControlTest / platformControlConfigure`，但自身不解释数据库驱动和秘密；其 Bootstrap 依赖于 Credentials 的部分降级为 soft，普通连接管理工作流在 Credentials 未就绪时仍然 fail-closed；
- Portal BFF 使用固定、CSRF 保护且受 Management Binding 和角色权限约束的 `/platform-control` 路由。授权页面只接收 systemd credential name 或 owner-only 文件绝对路径，不接收密码明文；
- 同一数据库全栈插件增加 Workbench Form Page，覆盖未配置、测试、初始化、Ready 和 Recovery 状态。当前非敏感 Profile 可在授权页面回填，语言切换仍由插件目录驱动。

开发编排器默认启用两阶段 Profile 路径、全新 Seed 无 Portal Activation 时的静态恢复入口，以及 Seed Recovery 可写动作仍属于 P4 的最后一个检查点。

P5 第一批迁移已删除 Connection Manager 的直接 JSON 状态文件。普通数据库管理请求通过统一 Workflow 进入按租户、Leader-fenced 的 Shared State CAS 聚合；连接定义、凭证候选、Runtime publication outbox 和回收队列保持原有原子边界。Platform Control 配置操作在 Shared State 尚未绑定时仍走独立受限内核端口，因此不会形成“为了配置数据库而先依赖数据库”的循环。
