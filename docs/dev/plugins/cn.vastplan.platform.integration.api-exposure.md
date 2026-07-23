# API Exposure 治理插件

插件 ID：`cn.vastplan.platform.integration.api-exposure`

当前制品版本：`0.4.0`
能力：`tool.package/platform.api-exposure`

## 职责

该插件拥有 API Exposure 和 Data Plane Exposure 的治理状态、Route Key、审批审计、Gateway Catalog、Endpoint Lease 与一次性 Ticket。它不实现业务 API，不解释插件代码，也不向公网暴露插件 ID。

可信 Contract Catalog 只能由宿主从已经校验发布者证明、制品摘要和清单的不可变制品生成。浏览器提交的是 Catalog 中精确 Contract Reference；控制面在创建和发布时都会重新解析，无法用请求体注入 capability 或替换目标。

HTTP Exposure 生命周期为 `Draft → PendingApproval → Approved → Published → Retired`。提交人与审批人必须不同；新 revision 保持原 Exposure ID 和 Route Key，旧 Published revision 变为 `Superseded`。退役后 Route Key 永久进入 tombstone。

Data Plane Exposure 使用相同职责分离流程，审批内容还必须固定允许的 HTTPS endpoint origins 与 SPIFFE 身份前缀。已发布后，贡献插件的运行实例才能登记最长 5 分钟且命中这些边界的 HTTPS Endpoint Lease。`ticket-redirect` 由 Portal 在 `/api/d/{routeKey}/ticket` 完成认证、权限和 CSRF 检查，再让控制面选择健康 Lease。控制面主动把 30 秒一次性 Ticket 安装到目标数据面，客户端只得到 endpoint 与不透明 Ticket；数据面本地消费，不需要在公开请求中反向调用控制面。

## 配置与状态

| 字段 | 必填 | 说明 |
|---|---:|---|
| `stateFile` | 是 | `0600`、非符号链接、最大 64 MiB 的治理状态 |
| `gatewayCatalogFile` | 是 | 原子替换的 Gateway Catalog |
| `contractCatalogFile` | 是 | 可信宿主生成的私有只读 Catalog 文件；避免把大型目录塞入进程环境 |

Endpoint Lease 与 Ticket 只保存在内存中，进程重启后失效；发布 revision、Route Key tombstone 和审计持久化。Catalog 使用 dirty journal 恢复，损坏或 generation 回退的候选不会覆盖 Gateway 最近可用快照。

## Portal

平台 Profile 把该插件绑定为 `platform.api-exposure` leader 服务。Portal Binding 只允许固定 operation 字典，用户还必须拥有 `platform.api-exposure.read/edit/approve/publish` 中对应角色。管理页面完全使用 Workbench Collection 和动态表单契约，不直接导入 React、Arco 或 MUI。

普通 HTTP 公开入口为 `/api/r/{routeKey}/v{major}/{contractPath}`；数据面 Ticket 入口为 `/api/d/{routeKey}/ticket`。公开响应和错误均不得包含插件、capability、逻辑服务、节点、NATS 或 gRPC 信息。

完整契约和安全顺序见《[API 暴露与数据面服务](../architecture/API暴露与数据面服务.md)》与 [ADR-0110](../decisions/ADR-0110-治理式API-Exposure与独立数据面.md)。
