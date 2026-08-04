# 数据库连接基础插件

插件 ID：`cn.vastplan.platform.data.relational.connection-manager`
能力：`tool.package/platform.database`
当前制品版本：`0.10.8`

## 边界

本插件管理租户隔离的数据库连接定义：稳定 `resourceId`、单调 revision、Provider、非敏感 options、连接池策略、端点、数据库名和不透明托管凭证引用。删除后只保留不含秘密的 identity/revision tombstone，因此同名重建仍递增 revision，不会被尚存旧池的副本当作版本回退。`define` 可接收一次性的只写 `credentialValue`，立即交给凭证插件加密托管；明文不进入连接状态文件、响应或日志。连接定义、凭证候选和 Runtime publication 都以可恢复状态收敛，状态文件使用 `0600` 原子替换。

`probe` 和发布现已调用 dedicated 的 `cn.vastplan.foundation.data.relational.runtime`，旧 `kernel.database.probe` 路径已移除。Runtime 负责真实 Provider、本地池和执行，本插件继续只做 leader 管理面；Kernel 不编入 PostgreSQL、MySQL 或其他数据库驱动。完整边界和实施顺序见 [ADR-0095](../decisions/ADR-0095-Database-Runtime多Provider连接池与集群事务.md)。

## 运行配置

第一方进程须从受控环境变量 `VASTPLAN_DATABASE_CONNECTIONS_STATE_FILE` 取得连接定义状态位置；Node Agent 必须显式列入环境白名单。文件必须位于持久卷，权限建议为 `0600`。插件采用 leader 运行策略，因此同一逻辑服务只有一个定义写入者。

## API

| 操作 | 含义 |
|---|---|
| `define` | 保存 Provider/options/池策略；新建时接收只写 `credentialValue`，编辑留空则保留原托管凭证，并创建待发布 revision |
| `describe`、`list` | 返回连接定义 |
| `remove` | 删除期望定义，并以 outbox 退役 Runtime revision 后再退役凭证 |
| `probe` | 让 Database Runtime 以加密 Material Lease 执行连通性检查 |

内部 `resolveRuntime` 不写入公开 descriptor，仅允许经过宿主认证的精确 Database Runtime 插件读取请求的现行 revision，供新副本惰性建池。它不会列出全部定义，也不能读取已删除或旧 revision。

没有返回或解密凭证的 API。Database Runtime、可信实例身份、访问策略或 Material Lease 任一不可用时，`probe` 都会 fail-closed。

## 当前与目标状态

连接定义、托管凭证 candidate Saga、Runtime publication outbox、Runtime 探测和 active-active 惰性收敛已经完成。列表中的 `runtime=ready|pending` 表示当前 revision 是否至少成功发布到一个副本；它不谎称所有副本已预热。每个 Runtime 副本拥有本地有界池，未预热副本在首次请求时自行收敛。事务亲和仍属于下一阶段。

## Portal 管理页

同一签名制品提供 `/settings/databases` 页面。0.5 已迁移到 Collection/Form Workbench，统一配置 PostgreSQL/MySQL 数据库类型、用户名、传输加密、连接超时、池预算和运行状态。用户通过受治理的 `secretMaterial` 直接输入一次性密码或令牌，不再复制 CredentialRef；新建必须输入，编辑永不回填且留空保留现有托管凭证，提交结束后 Workbench 删除浏览器状态中的材料引用。表单由 `FormDialog` 组合 Overlay，由动态表单持有 Schema、分段和字段状态；数据库插件显式选择 `horizontal + inline`，连接标识、连接选项和连接池均声明两列分区。前端表单投影将 `options/password` 紧跟用户名置于同一行，并统一显示为“密码”；提交工作流在调用 API 前必须从非敏感 `options` 中剥离该值，再映射为根级只写 `credentialValue`，因此持久化连接选项、响应和日志仍不包含明文。直接对象跨满分区后由 Renderer 将内部字段按同一两列模型排列。所有分段共享 Renderer 计算的 Label 列宽，Section 标题是唯一分组标题，不再绘制嵌套对象窗口或重复对象标题。中文环境中的功能性名称必须使用中文，PostgreSQL、MySQL、TCP、Unix 等产品或协议标识保留行业名称。详见《[插件配置与托管凭证](../architecture/插件配置与托管凭证.md)》。权限与集群调用见《[平台管理中心](../architecture/平台管理中心.md)》。
