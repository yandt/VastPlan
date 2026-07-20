# 数据库连接基础插件

插件 ID：`cn.vastplan.platform.data.relational.connection-manager`
能力：`tool.package/platform.database`
当前制品版本：`0.3.0`

## 边界

本插件管理租户隔离的数据库连接定义：驱动、端点、数据库名和不透明托管凭证引用。`define` 可接收一次性的只写 `credentialValue`，立即交给凭证插件加密托管；明文不进入连接状态文件、响应或日志。连接定义和凭证候选以可恢复 pending 状态收敛，状态文件使用 `0600` 原子替换。

`probe` 当前将非敏感定义和 `CredentialRef` 交给 `kernel.database.probe`。可信部署适配器实现 `kernelspi.DatabaseBroker`，并在内部通过 `CredentialBroker` 使用 Vault/KMS 凭证。插件无法取得、序列化或返回凭证明文。

目标架构由 dedicated 的 `cn.vastplan.foundation.data.relational.runtime` 基础插件负责真实 Provider、连接池、查询和集群事务；本插件继续只做管理面。迁移完成后稳定 wire capability 取代当前 Kernel Broker，Kernel 不编入 PostgreSQL、MySQL 或其他数据库驱动。完整边界和实施顺序见 [ADR-0095](../decisions/ADR-0095-Database-Runtime多Provider连接池与集群事务.md)。

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

## 当前与目标状态

当前已经完成连接定义、托管凭证 candidate Saga 和可信宿主 `probe` 边界。独立 Database Runtime 已完成 v1 wire 契约、Provider SPI、安全发现、可信实例 audience、Kernel 不见明文的 Material Lease 中继、统一 Pool Manager，以及 `postgresql`/`mysql` 真实 Provider；尚未把 connection-manager 发布闭环切换到 Runtime 外部执行服务。池不会放入 Kernel，后续数据库类型复用同一 Provider 契约。每个 Runtime 副本拥有本地有界池，事务通过不透明句柄固定路由到创建它的实例。

## Portal 管理页

同一签名制品提供 `/settings/databases` 页面。用户直接在连接表单中输入密码或令牌，不再先创建、再复制 CredentialRef 名称。页面读取只显示“已托管”，编辑时不会回填秘密；填写新值会创建并激活新凭证，删除连接会退役其托管句柄。详见《[插件配置与托管凭证](../architecture/插件配置与托管凭证.md)》。权限与集群调用见《[平台管理中心](../architecture/平台管理中心.md)》。
