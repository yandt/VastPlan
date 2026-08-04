# 数据库连接基础插件

插件 ID：`cn.vastplan.platform.data.relational.connection-manager`
能力：`tool.package/platform.database`
当前制品版本：`0.13.4`

## 边界

本插件管理租户隔离的数据库连接定义：稳定 `resourceId`、单调 revision、Provider、非敏感 options、连接池策略、端点、数据库名和不透明托管凭证引用。删除后只保留不含秘密的 identity/revision tombstone，因此同名重建仍递增 revision，不会被尚存旧池的副本当作版本回退。`define` 可接收一次性的只写 `credentialValue`，立即交给凭证插件加密托管；明文不进入连接状态文件、响应或日志。连接定义、凭证候选和 Runtime publication 都以可恢复状态收敛，状态文件使用 `0600` 原子替换。

`probe` 和发布现已调用 dedicated 的 `cn.vastplan.foundation.data.relational.runtime`，旧 `kernel.database.probe` 路径已移除。Runtime 负责真实 Provider、本地池和执行，本插件继续只做 leader 管理面；Kernel 不编入 PostgreSQL、MySQL 或其他数据库驱动。完整边界和实施顺序见 [ADR-0095](../decisions/ADR-0095-Database-Runtime多Provider连接池与集群事务.md)。

## 测试连接错误边界

`test` 使用短期托管凭证调用 Runtime 的一次性 `probe`，不保存连接定义。Runtime 的稳定诊断码在管理面保持一一对应，Portal 再转换为浏览器稳定码：参数无效、部署 TLS 策略、DNS 解析、拒绝连接、连接超时、证书校验、身份验证、数据库不存在、权限不足和连接池耗尽均能得到明确、可本地化的排查提示。无法归类的连接故障和 Runtime 故障继续使用安全兜底码。

浏览器只接收稳定错误码，不接收数据库地址、密码、DSN、驱动原文或 TLS 握手细节。可信 Runtime 日志保留脱敏后的技术分类和驱动码，并关联 trace ID；前端根据插件语言目录生成面向用户的可操作提示。前端在提交前还会剔除切换 Provider 后遗留的隐藏字段，并明确校验用户名，避免有效连接被其他 Provider 的字段拒绝。

## 运行配置

第一方进程须从受控环境变量 `VASTPLAN_DATABASE_CONNECTIONS_STATE_FILE` 取得连接定义状态位置；Node Agent 必须显式列入环境白名单。文件必须位于持久卷，权限建议为 `0600`。插件采用 leader 运行策略，因此同一逻辑服务只有一个定义写入者。

## API

| 操作 | 含义 |
|---|---|
| `define` | 保存 Provider/options/池策略；新建时接收只写 `credentialValue`，编辑留空则保留原托管凭证，并创建待发布 revision |
| `describe`、`list` | 返回连接定义 |
| `remove` | 删除期望定义，并以 outbox 退役 Runtime revision 后再退役凭证 |
| `probe` | 让 Database Runtime 以加密 Material Lease 执行连通性检查 |
| `test` | 使用当前表单值测试连接；新密码经可恢复的短期托管凭证进入 Runtime，不保存连接定义 |

内部 `resolveRuntime` 不写入公开 descriptor，仅允许经过宿主认证的精确 Database Runtime 插件读取请求的现行 revision，供新副本惰性建池。它不会列出全部定义，也不能读取已删除或旧 revision。

没有返回或解密凭证的 API。Database Runtime、可信实例身份、访问策略或 Material Lease 任一不可用时，`probe` 都会 fail-closed。

## 当前与目标状态

连接定义、托管凭证 candidate Saga、Runtime publication outbox、Runtime 探测和 active-active 惰性收敛已经完成。列表中的 `runtime=ready|pending` 表示当前 revision 是否至少成功发布到一个副本；它不谎称所有副本已预热。每个 Runtime 副本拥有本地有界池，未预热副本在首次请求时自行收敛。事务亲和仍属于下一阶段。

## Portal 管理页

同一签名制品提供 `/settings/databases` 页面。0.5 已迁移到 Collection/Form Workbench，统一配置 PostgreSQL/MySQL 数据库类型、用户名、传输加密、连接超时、池预算和运行状态。用户通过受治理的 `secretMaterial` 直接输入一次性密码或令牌，不再复制 CredentialRef；新建 Schema 将密码声明为必填并使用统一字段校验，不再显示重复帮助提示；编辑永不回填密码且留空保留现有托管凭证。提交结束后 Workbench 删除浏览器状态中的材料引用。表单由 `FormDialog` 组合 Overlay，由动态表单持有 Schema、分段和字段状态；数据库插件显式选择 `horizontal + inline`，连接标识、连接选项和连接池均声明两列分区。前端表单投影将 `options/password` 紧跟用户名置于同一行，并统一显示为“密码”；提交工作流在调用 API 前必须从非敏感 `options` 中剥离该值，再映射为根级只写 `credentialValue`，因此持久化连接选项、响应和日志仍不包含明文。“证书校验服务器名称”通过声明式 `visibleWhen` 仅在传输加密模式为完整校验时显示，关闭传输加密后即时隐藏。连接、读写、获取和池生命周期等毫秒字段统一使用 Workbench `duration` widget：后端及 JSON Schema 继续保存毫秒整数，界面按字段限制显示可选单位；短超时只开放毫秒、秒、分，池生命周期开放至小时、天或周。月份按固定 30 天换算，不表达日历月份。直接对象跨满分区后由 Renderer 将内部字段按同一两列模型排列。所有分段共享 Renderer 计算的 Label 列宽，Section 标题是唯一分组标题，不再绘制嵌套对象窗口或重复对象标题。中文环境中的功能性名称必须使用中文，PostgreSQL、MySQL、TCP、Unix 等产品或协议标识保留行业名称。

0.11.1 将既有 `probe` 数据链产品化为“测试连接”。列表行动作复用已保存定义与托管凭证；FormDialog 的 `footer.start` 动作使用当前表单值：编辑且密码留空时复用现有托管引用，新建或输入新密码时通过随机资源名创建短期托管凭证。凭证仍处于 Preparing 时就先耐久记录 `stageId + ref` 清理意图，再执行激活；测试完成立即终止候选或退役 Active，进程在任意步骤中断后由 reconcile 流程继续清理。两条路径都由 Database Runtime 创建短生命周期池并执行 Provider `Probe`，不保存表单定义、不发布 revision，也不污染正式连接池。Workbench 复用统一校验、等待和通知；成功提示实际 Provider 和握手耗时，失败仅给出安全的本地化排查建议，不向浏览器透传驱动、地址、账号或凭证诊断。

0.12.0 将 Portal 的 TCP 端点输入拆为“地址”和“端口”：PostgreSQL 新建默认 5432，MySQL 的 host-only 历史定义按 3306 显示。提交前前端将两字段规范化为唯一的 `endpoint`，因此管理 API、持久化状态与 Database Runtime 无需承载 UI 字段。IPv6 在表单中输入裸地址，提交时自动以方括号编码；地址字段已含端口、空地址或 1..65535 以外的端口均被本地化字段校验拒绝。Unix Socket 仍是 Provider 专有运行方式，未被错误地伪装为 TCP 地址加端口。

0.12.1 将数据库用户名和密码标记为浏览器的 `new-password` 字段。这些字段不再被识别为 Portal 登录凭证，因此浏览器不会把本站保存的登录账号自动回填到数据库连接表单；该策略不影响 Portal 登录页。

详见《[插件配置与托管凭证](../architecture/插件配置与托管凭证.md)》。权限与集群调用见《[平台管理中心](../architecture/平台管理中心.md)》。
