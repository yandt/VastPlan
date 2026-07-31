# Portal Composer

插件 ID：`cn.vastplan.platform.configuration.portal-composer`

当前制品版本：`4.0.7`

该平台基础插件以 `active-active + external-shared + queue` 方式治理 Portal。每个 `portalId` 只有一个聚合，内部包含 WorkingCopy、Publication、Release 和版本控制语义状态。

主要能力：

- WorkingCopy revision CAS 保存，不产生业务版本或历史；
- 提交时冻结完整配置和 digest，形成 `PendingApproval → Approved → Published` Publication；
- `PortalRelease` 上线、历史回滚、制品物化、引用保护和 CAS；普通上线只接受最近且从未上线的 Published Publication；
- Frontend Test Release 在完整 Portal 配置上替换一个获授权插件，并形成带原子归属的隔离候选；候选与 Activation 不进入正式 Publication/PortalRelease 谱系，不能由普通上线入口晋级；
- 只通过 `kernel.portal.catalog.*` 窄服务取得可信校验与已验签制品引用；
- `referencePending` outbox 在管理读取和进程重启后幂等收敛；
- Portal 用户偏好按 tenant、subject 与 Portal scope 独立保存。

组合治理状态使用 v4 namespace 下的“小型 Root CAS + 512 KiB 内容寻址 chunk”。Root 是唯一提交点并绑定完整快照 SHA-256，读路径逐块复核大小和摘要。当前处于开发阶段，不读取或迁移早期状态；单租户快照安全上限为 64 MiB。

静态 `portal-platform-catalog.json` 仅为首个 Portal WorkingCopy 提供种子配置，不再形成可在线编辑的 Platform Profile 或 PortalBinding。治理读取会返回可信 `creationTemplate`，因此租户尚无 Portal 时也能创建第一条记录；空上线历史统一编码为 `[]`。内核 Recovery Baseline 继续独立于 Portal，确保错误配置不能破坏最小管理与恢复入口。

管理中心只注册 `/settings/portals` 一个 Workbench Collection。一行代表一个 Portal；编辑表单一次修改完整配置，版本历史、上线历史、审计和完整配置通过该行的子视图打开。独立 Profile、Binding 和 Activation 菜单及浏览器 API 已删除。

可信 BFF API：

- `GET|POST /v1/portals`：读取聚合或创建 Portal；
- `POST /v1/portals/{portalId}/working-copy`：从已发布配置创建下一轮工作副本；
- `PUT /v1/portals/{portalId}/working-copy`：按 working revision CAS 保存完整配置；
- `POST /v1/portals/{portalId}/publications`：冻结当前 WorkingCopy 并提交审批；
- `POST /v1/portals/{portalId}/publications/{publicationId}/approve|publish`：推进 Publication；
- `POST /v1/portals/{portalId}/releases`：上线一个 Published Publication；
- `POST /v1/portals/{portalId}/releases/{releaseId}/rollback`：由历史 Release 创建新上线记录；
- `GET /v1/portals/{portalId}/history[/{versionId}]`、`GET .../compare`、`POST .../restore`：仅在配置 Workspace 后提供历史、比较和恢复。

所有写操作均重新取得 CSRF token；tenant 与 Principal 只能由可信会话和 CallContext 投影。Portal、Publication 与 Release 身份以 URL 为权威并在 Composer 交叉校验，正文不能覆盖；system break-glass 必须携带原因并写入高优先级审计。完整边界见《[前端门户内核](../architecture/前端门户内核.md)》、[ADR-0174](../decisions/ADR-0174-Portal可选版本控制与发布快照分离.md) 和 [ADR-0125](../decisions/ADR-0125-Portal-Composer与Preference共享状态分区.md)。

## P2.3 实施状态

P2.3a 至 P2.3e 已完成：后端聚合、中立版本控制端口、Workspace Adapter、BFF、TypeScript SDK 与 Workbench 均使用 WorkingCopy/Publication/Release；旧 `/versions` 与 PortalVersion 用户操作已删除。确定性故障矩阵已覆盖无版本与 Workspace 模式、能力差异、惰性依赖缺失、跨重启幂等恢复、聚合冲突、冷历史故障和 Release 独立性。真实进程 E2E 会同时启动 Portal Composer、Version Workspace、Version Ledger 与 Node Portal Kernel，并经浏览器 BFF 验证提交、历史、比较、恢复和上线完整链；长期混沌与 soak 留待具备真实插件负载后执行。
