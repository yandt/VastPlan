# 数据库连接基础插件

插件 ID：`cn.vastplan.platform.data.relational.connection-manager`
能力：`tool.package/platform.database`
当前制品版本：`0.2.0`

## 边界

本插件管理租户隔离的数据库连接定义：驱动、端点、数据库名和凭证引用。它不接受密码、连接串中的凭证片段或任意 SQL；连接定义以 `0600` 的原子状态文件持久化。

`probe` 将非敏感定义和 `CredentialRef` 交给 `kernel.database.probe`。可信部署适配器实现 `kernelspi.DatabaseBroker`，并在内部通过 `CredentialBroker` 使用 Vault/KMS 凭证。插件无法取得、序列化或返回凭证明文。

这使连接产品、数据库驱动和凭证使用策略可以由部署适配器独立演进，内核只保留稳定的 `DatabaseBroker` SPI 与经过认证的宿主回调边界。

## 运行配置

第一方进程须从受控环境变量 `VASTPLAN_DATABASE_CONNECTIONS_STATE_FILE` 取得连接定义状态位置；Node Agent 必须显式列入环境白名单。文件必须位于持久卷，权限建议为 `0600`。插件采用 leader 运行策略，因此同一逻辑服务只有一个定义写入者。

## API

| 操作 | 含义 |
|---|---|
| `define` | 保存 `name/driver/endpoint/database/credential`，不含密码 |
| `describe`、`list` | 返回连接定义 |
| `remove` | 删除连接定义 |
| `probe` | 让可信宿主以凭证引用执行连通性检查 |

没有返回或解密凭证的 API。生产部署未注入 `DatabaseBroker` 时，`probe` 会 fail-closed。

## Portal 管理页

同一签名制品提供 `/settings/databases` 页面，管理非敏感连接定义并触发可信宿主 probe。CredentialRef 只填写凭证名称；页面和 BFF 均没有密码字段。权限与集群调用见《[平台管理中心](../architecture/平台管理中心.md)》。
