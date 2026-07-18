# 节点部署管理服务

插件 ID：`com.vastplan.platform.infrastructure.deployment-manager`

该 platform 基础插件以 `leader + leader-owned + cluster + platform routing domain` 运行，持有租户隔离的节点计划、Bootstrap Job、服务组合 revision 和审计记录。它依赖 settings、credentials、artifact repository 与窄内核服务，但只保存 Credential 名称和 Application Composition，永远不能读取 SSH/NATS/制品令牌 material、Platform Catalog、信任根或 KV 句柄。

当前 capability 为 `platform.deployment`：

- `listNodes`、`putNode`：查询或以 CAS 保存节点计划；
- `listBootstrapJobs`：查询首次引导状态；
- `createBootstrap`：由 `platform.deployment.bootstrap` 角色申请；
- `approveBootstrap`：由不同的 `platform.deployment.approve` 用户批准并触发可信宿主。
- `listDeploymentTargets`、`listServiceRevisions`、`listServiceRevisionAudit`：读取预授权槽位和服务组合记录；
- `create/update/submitServiceDraft`：由 `platform.deployment.compose` 编辑并提交仅含应用插件的组合；
- `approveServiceRevision`：由不同的 `platform.deployment.approve` 用户批准；
- `publish/rollbackServiceRevision`：由 `platform.deployment.publish` 通过可信内核发布或创建新 revision 回滚。

活动作业期间节点定义被冻结。进程重启时，未确认的 `Connecting/Installing` 会落为 `Failed` 且不自动重放，避免高权限 SSH 被重复执行。服务发布采用不同语义：`Publishing` 可用同 revision/同摘要幂等重试。`kernel.deployment.targets/preview/publish` 只接受精确插件身份，并由内核固定 Profile、验签制品和 CAS 写入。状态文件配置、运行说明见插件目录 [README](../../../extensions/plugins/com.vastplan.platform.infrastructure.deployment-manager/README.md)，完整边界见《[服务部署控制台](../architecture/服务部署控制台.md)》、[ADR-0070](../decisions/ADR-0070-Deployment-Manager与可信引导执行边界.md)、[ADR-0071](../decisions/ADR-0071-签名Node-Lease与可信就绪判定.md) 与 [ADR-0077](../decisions/ADR-0077-Backend在线组合与可信发布边界.md)。
