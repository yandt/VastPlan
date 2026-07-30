# Portal Composer

插件 ID：`cn.vastplan.platform.configuration.portal-composer`

当前制品版本：`2.0.1`

该平台基础插件以 `active-active + external-shared + queue` 方式治理 Portal。对管理员只暴露一个聚合：每个 `portalId` 一条 Portal 记录，完整平台设置、应用插件、路由、品牌和管理服务绑定均保存在其 `PortalVersion` 内。

主要能力：

- Portal 创建及完整配置版本管理；同一 Portal 最多一个未发布候选；
- `Draft → PendingApproval → Approved → Published` 异人审批，Published 只冻结版本，不直接上线；
- `PortalRelease` 上线、历史回滚、制品物化、引用保护和 CAS；普通上线只接受最新且从未上线的 Published 版本，历史恢复必须走显式回滚；
- Frontend Test Release 在完整 Portal 配置上替换一个获授权插件，并形成带原子归属的隔离候选；候选版本和 Activation 不进入正式 PortalVersion/PortalRelease 谱系，不能占用正式版本号或由普通上线入口晋级；
- 只通过 `kernel.portal.catalog.*` 窄服务取得可信校验与已验签制品引用；
- `referencePending` outbox 在管理读取和进程重启后幂等收敛；
- Portal 用户偏好按 tenant、subject 与 Portal scope 独立保存。

组合治理状态使用“小型 Root CAS + 512 KiB 内容寻址 chunk”。Root 是唯一提交点并绑定完整快照 SHA-256，读路径逐块复核大小和摘要；旧的单值租户文档保持可读，并在下一次成功 CAS 时原位升级。当前单租户快照安全上限为 64 MiB，不再受 Shared State 单值 1 MiB 限制。

静态 `portal-platform-catalog.json` 仅为首个 PortalVersion 提供种子配置，不再形成可在线编辑的 Platform Profile 或 PortalBinding。治理读取会返回可信 `creationTemplate`，因此租户尚无 Portal 时也能创建第一条记录；空版本和上线历史统一编码为 `[]`。内核 Recovery Baseline 继续独立于 Portal，确保错误配置不能破坏最小管理与恢复入口。

管理中心只注册 `/settings/portals` 一个 Workbench Collection。一行代表一个 Portal；编辑表单一次修改完整配置，版本历史、上线历史、审计和完整配置通过该行的子视图打开。独立 Profile、Binding 和 Activation 菜单及浏览器 API 已删除。

可信 BFF API：

- `GET|POST /v1/portals`：读取聚合或创建 Portal；
- `POST /v1/portals/{portalId}/versions`：创建新版本；
- `PUT|DELETE /v1/portals/{portalId}/versions/{versionId}`：编辑或删除草稿；
- `POST .../submit|approve|publish`：推进版本状态；
- `POST /v1/portals/{portalId}/releases`：上线一个 Published PortalVersion；
- `POST /v1/portals/{portalId}/releases/{releaseId}/rollback`：由历史版本创建新上线记录。

所有写操作均重新取得 CSRF token；tenant 与 Principal 只能由可信会话和 CallContext 投影。Portal、Version 与 Release 身份只取 URL 并在 Composer 交叉校验，正文不能覆盖；system break-glass 必须携带原因并写入高优先级审计。完整边界见《[前端门户内核](../architecture/前端门户内核.md)》、[ADR-0171](../decisions/ADR-0171-Portal单聚合版本与上线子流程.md) 和 [ADR-0125](../decisions/ADR-0125-Portal-Composer与Preference共享状态分区.md)。

## P2.3 规划说明

上文描述当前 `2.0.1` 制品。P2.3 将按 [Portal 可选版本控制接入](../architecture/Portal可选版本控制接入.md)进行开发期破坏性升级：PortalVersion 拆为 WorkingCopy、Publication、Release 与可选 VersionControlBinding。未配置 Workspace 时没有通用版本历史，但审批冻结和 Release 安全语义继续存在；该规划尚未进入当前插件代码和 BFF API。
