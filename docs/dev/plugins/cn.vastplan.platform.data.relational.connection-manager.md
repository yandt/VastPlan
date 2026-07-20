# 数据库连接基础插件

插件 ID：`cn.vastplan.platform.data.relational.connection-manager`
能力：`tool.package/platform.database`
当前制品版本：`0.3.0`

## 边界

本插件管理租户隔离的数据库连接定义：驱动、端点、数据库名和不透明托管凭证引用。`define` 可接收一次性的只写 `credentialValue`，立即交给凭证插件加密托管；明文不进入连接状态文件、响应或日志。连接定义和凭证候选以可恢复 pending 状态收敛，状态文件使用 `0600` 原子替换。

`probe` 将非敏感定义和 `CredentialRef` 交给 `kernel.database.probe`。可信部署适配器实现 `kernelspi.DatabaseBroker`，并在内部通过 `CredentialBroker` 使用 Vault/KMS 凭证。插件无法取得、序列化或返回凭证明文。

这使连接产品、数据库驱动和凭证使用策略可以由部署适配器独立演进，内核只保留稳定的 `DatabaseBroker` SPI 与经过认证的宿主回调边界。

## 运行配置

第一方进程须从受控环境变量 `VASTPLAN_DATABASE_CONNECTIONS_STATE_FILE` 取得连接定义状态位置；Node Agent 必须显式列入环境白名单。文件必须位于持久卷，权限建议为 `0600`。插件采用 leader 运行策略，因此同一逻辑服务只有一个定义写入者。

## API

| 操作 | 含义 |
|---|---|
| `define` | 保存连接；新建时接收只写 `credentialValue`，编辑留空则保留原托管凭证 |
| `describe`、`list` | 返回连接定义 |
| `remove` | 删除连接定义 |
| `probe` | 让可信宿主以凭证引用执行连通性检查 |

没有返回或解密凭证的 API。生产部署未注入 `DatabaseBroker` 时，`probe` 会 fail-closed。

## Portal 管理页

同一签名制品提供 `/settings/databases` 页面。用户直接在连接表单中输入密码或令牌，不再先创建、再复制 CredentialRef 名称。页面读取只显示“已托管”，编辑时不会回填秘密；填写新值会创建并激活新凭证，删除连接会退役其托管句柄。详见《[插件配置与托管凭证](../architecture/插件配置与托管凭证.md)》。权限与集群调用见《[平台管理中心](../architecture/平台管理中心.md)》。
