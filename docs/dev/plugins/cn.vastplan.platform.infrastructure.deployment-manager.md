# 节点部署管理服务

插件 ID：`cn.vastplan.platform.infrastructure.deployment-manager`

当前制品版本：`0.8.0`

该 platform 基础插件以 `leader + leader-owned + cluster + platform routing domain` 运行，持有租户隔离的节点计划、Bootstrap Job、服务组合 revision、Test Target Binding、Test Release 和审计记录。它依赖 settings、credentials、artifact repository 与窄内核服务，但只保存 Credential 名称、Application Composition 和精确制品身份，永远不能读取 SSH/NATS/制品令牌 material、Platform Catalog、信任根或 KV 句柄。

当前 capability 为 `platform.deployment`：

- `listNodes`、`putNode`：查询或以 CAS 保存节点计划；
- `listBootstrapJobs`：查询首次引导状态；
- `createBootstrap`：由 `platform.deployment.bootstrap` 角色申请；
- `approveBootstrap`：由不同的 `platform.deployment.approve` 用户批准并触发可信宿主。
- `listDeploymentTargets`、`listServiceRevisions`、`listServiceRevisionAudit`：读取预授权槽位和服务组合记录；
- `create/update/submitServiceDraft`：由 `platform.deployment.compose` 编辑并提交仅含应用插件的组合；
- `approveServiceRevision`：由不同的 `platform.deployment.approve` 用户批准；
- `publish/rollbackServiceRevision`：由 `platform.deployment.publish` 通过可信内核发布或创建新 revision 回滚。
- `list/putTestTargetBinding`：读取或由 `platform.deployment.test-target` 以 CAS 预授权 Backend 应用插件测试槽位；
- `list/createTestRelease`：读取记录，或由 `platform.deployment.publish` 提交精确 testing 制品并等待候选结果；
- `rollbackTestRelease`：恢复回滚被控制器重启中断且已标记 `rollbackRequired` 的发布。

以上权限及 19 个用户管理 operation 已进入插件签名 Manifest 的 `authorization` 目录。服务编辑、审批、发布/回滚和测试目标授权保持分离；Manifest 的 `different-subject` 只提供策略元数据，提交人与审批人分离仍由本服务的持久状态机最终强制。

活动作业期间节点定义被冻结。进程重启时，未确认的 `Connecting/Installing` 会落为 `Failed` 且不自动重放，避免高权限 SSH 被重复执行。服务发布采用不同语义：`Publishing` 可用同 revision/同摘要幂等重试；中断的 Test Release 则 fail-closed 并要求显式恢复回滚。`kernel.deployment.targets/preview/publish/readiness` 只接受精确插件身份，并由内核固定 Profile、验签制品、CAS 写入和判断真实收敛。状态文件配置、运行说明见插件目录 [README](../../../extensions/plugins/cn.vastplan.platform.infrastructure.deployment-manager/README.md)，完整边界见《[服务部署控制台](../architecture/服务部署控制台.md)》、《[制品仓库与测试发布](../architecture/制品仓库与测试发布.md)》、[ADR-0070](../decisions/ADR-0070-Deployment-Manager与可信引导执行边界.md)、[ADR-0071](../decisions/ADR-0071-签名Node-Lease与可信就绪判定.md)、[ADR-0077](../decisions/ADR-0077-Backend在线组合与可信发布边界.md) 与 [ADR-0097](../decisions/ADR-0097-测试制品仓库与前端分级热升级.md)。

0.5.0 起，服务发布在内核切换前先提交“旧活动 + 新候选”引用并集，切换成功后先固化回滚引用、再收敛活动引用；任一步失败只会多保护对象。精确引用同步由持久化 `referencePending` outbox 驱动，仓库恢复后在管理读取路径自动幂等重试，控制器重启也会重新校验活动 revision。Backend Test Release 在候选激活前还会发布独立的精确 artifact-lock owner。仓库不可用时候选 fail-closed，GC 不会获得一个缺引用但看似健康的窗口。

0.6.0 起，部署预览由可信内核返回跨 Seed、托管仓库等来源解析后的精确制品引用；Deployment Manager 只消费并持久化该结果，不再旁路查询某一个仓库。这样引用保护与实际部署解析使用同一份事实，也避免 Seed 基础插件被误判为托管仓库缺失。

0.7.0 起，服务组合页面完全使用 Workbench Collection、动态 Form 与 Overlay 契约；部署目标枚举只在抽屉打开时加载，编辑和生命周期动作按所选 revision 状态显示，最终预览与审计不再由功能插件直接拼装 UI。
