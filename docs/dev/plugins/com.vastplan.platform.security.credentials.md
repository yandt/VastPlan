# 凭证管理基础插件

插件 ID：`com.vastplan.platform.security.credentials`
能力：`tool.package/platform.credentials`
运行模型：`leader + leader-owned + cluster + leader`
当前制品版本：`0.2.0`

## 安全模型

本插件采用 Vault Transit/KMS 信封加密：调用 `put` 时的明文只被编码后发送到 Vault Transit 的 `encrypt` API；插件状态文件仅保存 Vault 返回的密文、版本、时间与撤销状态。`describe`、`list`、`rotate`、`revoke` 的协议响应均不包含密文，更不包含明文。

`rotate` 使用 Transit `rewrap`，因此只轮换包裹密钥版本，不需要先解密再重新加密原始凭证。`revoke` 使引用失效；是否同时吊销外部数据库用户、API token 等目标系统身份，属于后续受控凭证操作与业务工作流，不由本插件猜测。

## 运行配置

凭证插件需要由 Node Agent 显式允许以下受控环境变量传入其**第一方可信进程**。token 不进入 DesiredState、插件状态文件、日志或协议返回值。

| 变量 | 含义 |
|---|---|
| `VASTPLAN_CREDENTIALS_STATE_FILE` | 凭证密文元数据的持久状态文件 |
| `VASTPLAN_VAULT_ADDR` | Vault HTTPS 地址 |
| `VASTPLAN_VAULT_TRANSIT_KEY` | Transit 包裹密钥名称 |
| `VASTPLAN_VAULT_TOKEN_FILE` | 只读 token 挂载文件（建议 `0600`） |

生产部署必须使用 HTTPS、最小 Vault policy、短期/可轮换工作负载 token 和持久卷。leader fencing 约束同一逻辑服务的写入者；状态卷复制与 Vault 高可用仍由部署层提供。

## API

所有操作都按 `CallContext.tenant_id` 隔离。

| 操作 | 作用 |
|---|---|
| `put(name, value)` | 将明文交给 Vault Transit 加密并保存新密文版本 |
| `describe(name)` | 返回名称、版本、密钥版本、时间和撤销状态 |
| `list(prefix)` | 返回当前租户的元数据列表 |
| `rotate(name)` | 调用 Transit rewrap 轮换包裹密钥 |
| `revoke(name)` | 撤销凭证引用 |

该 API **没有** `get` 或 `decrypt` 操作。数据库服务不能向它索取明文；后续数据库适配将通过可信宿主的受限操作使用 `CredentialRef`。

## Portal 管理页

同一签名制品提供 `/settings/credentials` 页面。列表只渲染 `Metadata`；保存字段使用 password widget，明文只进入 TLS + CSRF 写请求，请求完成后立即从编辑状态清空。轮换与撤销使用独立角色，详见《[平台管理中心](../architecture/平台管理中心.md)》。
