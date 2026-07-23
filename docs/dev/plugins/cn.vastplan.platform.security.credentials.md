# 凭证管理基础插件

插件 ID：`cn.vastplan.platform.security.credentials`
能力：`tool.package/platform.credentials`、`tool.package/platform.credentials.material-lease`
运行模型：`leader + external-shared + cluster + leader`
当前制品版本：`0.12.0`

## 安全模型

本插件采用 Vault Transit/KMS 信封加密：调用 `put` 时的明文只被编码后发送到 Vault Transit 的 `encrypt` API；Shared State 内容寻址快照仅保存 Vault 返回的密文、版本、时间与撤销状态。每租户小型 Root 是唯一 CAS 提交点，完整快照拆为不超过 512 KiB 的不可变 SHA-256 chunk，因此单个大 ciphertext 不受 Shared State 单值 1 MiB 限制。`describe`、`list`、`rotate`、`revoke` 的协议响应均不包含密文，更不包含明文。

可信宿主需要执行数据库连接等操作时，使用独立的 `platform.credentials.material-lease/issue`。该操作只接受认证后的 `SYSTEM` caller，以一次性 X25519 公钥签发默认 15 秒的 AES-GCM 加密信封，并把 tenant、宿主 audience、完整托管引用和时间窗绑定进 AAD。Vault decrypt 完成后插件会重新读取最新 Root，复核 handle、完整引用、状态和 ciphertext；跨实例 revoke/retire 优先于签发。它不提供返回明文的 API，用户和普通插件均不能申请。完整取舍见 [ADR-0093](../decisions/ADR-0093-可信宿主加密Material-Lease.md) 与 [ADR-0127](../decisions/ADR-0127-Credentials内容寻址快照与Material-Lease复核.md)。

`rotate` 使用 Transit `rewrap`，因此只轮换包裹密钥版本，不需要先解密再重新加密原始凭证。`revoke` 使引用失效；是否同时吊销外部数据库用户、API token 等目标系统身份，属于后续受控凭证操作与业务工作流，不由本插件猜测。

## 运行配置

凭证插件需要由 Node Agent 显式允许以下受控环境变量传入其**第一方可信进程**。token 不进入 DesiredState、插件状态文件、日志或协议返回值。

| 变量 | 含义 |
|---|---|
| `VASTPLAN_VAULT_ADDR` | Vault HTTPS 地址 |
| `VASTPLAN_VAULT_TRANSIT_KEY` | Transit 包裹密钥名称 |
| `VASTPLAN_VAULT_TOKEN_FILE` | 只读 token 挂载文件（建议 `0600`） |

生产部署必须使用 HTTPS、最小 Vault policy、短期/可轮换工作负载 token、三副本 Shared State Provider 和 Vault 高可用。插件不持有 NATS/SQL 凭证，Provider 不可用时 fail-closed，绝不回退本地文件。Root、全部不可变 chunk、GC marker 与控制器状态进入同一备份/恢复边界。Root、chunk 和 GC mutation 只能通过当前 Unit Leader evidence 约束的 fenced Shared State 能力执行；epoch/token 不进入插件 payload。

`configuration.maintenance` 控制托管候选维护，默认 Preparing 24 小时、Aborted 保留 30 天、审计保留 180 天、每小时最多对每租户执行一次、每轮最多 200 条。孤儿 chunk GC 默认宽限 24 小时、每次请求最多推进 100 条，安全范围分别为 1 小时至 30 天和 1 至 200 条。GC 以 `mark -> sweep -> idle` 跨请求恢复；删除前重新读取当前 Root 并复核 blob revision 和摘要，重新可达的 chunk 只清 marker。Shared State tenant scope 禁止后台自报 tenant，因此维护由该租户的任意受信请求有界触发；未来无请求租户的定时回收必须使用可信 tenant-scoped scheduler。完整取舍见 [ADR-0132](../decisions/ADR-0132-Credentials孤儿Chunk安全回收.md)。

## API

所有操作都按 `CallContext.tenant_id` 隔离。

| 操作 | 作用 |
|---|---|
| `put(name, value)` | 将明文交给 Vault Transit 加密并保存新密文版本 |
| `describe(name)` | 返回名称、版本、密钥版本、时间和撤销状态 |
| `list(prefix)` | 返回当前租户的元数据列表 |
| `listManagedAudit(beforeId, limit)` | 仅安全管理员：返回脱敏生命周期事件与维护状态，不返回 handle、stage ID、authority、密文或 material |
| `rotate(name)` | 调用 Transit rewrap 轮换包裹密钥 |
| `revoke(name)` | 撤销凭证引用 |
| `stageManaged(purpose, resource, value)` | 仅限已认证业务插件，创建 Preparing 候选并返回随机句柄 |
| `stageDelegated(authority, value)` | 仅配置协调器：原子消费宿主一次性授权，按 claims 派生目标 owner/purpose/resource 后暂存 |
| `prepareDelegated(stageId, candidateId)` | 仅配置协调器：打开候选 Deployment 可用、失败时仍可终止的 Candidate 窗口 |
| `activateManaged(stageId)` | 仅允许创建该候选的插件激活，重复调用幂等 |
| `abortManaged(stageId)` | 终止未激活候选并删除其密文 |
| `activateDelegated(stageId, candidateId)` | 仅配置协调器：激活精确候选绑定的委托凭证 |
| `abortDelegated(stageId, candidateId)` | 仅配置协调器：终止精确候选绑定的委托凭证 |
| `retireManaged(handle)` | 由所有者插件退役不再使用的 Active 句柄 |
| `material-lease/issue(ref, recipientPublicKey)` | 仅可信宿主：将 Candidate 或 Active 托管凭证重加密给本次一次性公钥；签发前后均复核状态 |

普通托管操作的 owner 从宿主认证后的插件 caller 注入；委托操作的 owner/purpose/resource 只来自 `kernel.configuration.authority.consume` 返回的活动目录 claims，payload 都不能指定或冒充。原始 authority 只使用一次且不进入状态文件。该 API **没有**面向普通插件的 `get` 或 `decrypt` 操作。数据库插件不能索取明文；可信宿主适配器按 tenant、owner、purpose 和 version 校验后，仅在受限同步回调内使用 `CredentialRef`。

## Portal 管理页

同一签名制品提供 `/settings/credentials` 和 `/settings/credentials-audit` 页面。前者的列表只渲染 `Metadata`；保存字段必须使用受治理的 `secretMaterial`，并由 Schema 同时声明 `format: vastplan-secret-material + writeOnly`。明文只进入 TLS + CSRF 写请求，不进入初始值、loader、偏好或脏状态 baseline；无论提交成功还是失败均立即从 Workbench 状态删除。后者只显示短指纹、状态、owner、purpose、resource 和维护统计，并要求独立 `platform.credentials.audit` 权限。轮换与撤销是受治理的行操作，详见《[平台管理中心](../architecture/平台管理中心.md)》。

该独立页面将收敛为安全管理员的审计、轮换和应急撤销视图。普通业务配置不再要求用户先来此创建名称：数据库、制品仓库等插件在自己的配置页声明并采集 `managedCredentials`，由配置协调器交给本插件托管。完整状态机见《[插件配置与托管凭证](../architecture/插件配置与托管凭证.md)》。Vault 工作负载 token 是自举根凭证，仍由部署层安全挂载，不能由本插件托管自身。
