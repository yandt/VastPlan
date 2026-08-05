# 节点部署管理服务

插件 ID：`cn.vastplan.platform.infrastructure.deployment-manager`

当前制品版本：`0.28.0`

该 platform 基础插件以 `leader + external-shared + cluster + leader routing` 运行，持有租户隔离的节点计划、Bootstrap Job、Application Intent/Plan 快照、服务组合 revision、Test Target Binding、Test Release 和审计记录。它依赖 settings、credentials、artifact repository、Composition Planner 与窄内核服务，但只保存不透明 CredentialRef、已编译 Application Composition 和精确制品身份，永远不能读取 SSH/NATS/制品令牌 material、Platform Catalog、信任根或 KV 句柄。

当前 capability 为 `platform.deployment`：

- `listNodes`、`putNode`：查询或以 CAS 保存节点计划；
- `listBootstrapJobs`：查询首次引导状态；
- `createBootstrap`：由 `platform.deployment.bootstrap` 角色申请；
- `approveBootstrap`：由不同的 `platform.deployment.approve` 用户批准并触发可信宿主。
- `listDeploymentTargets`、`listServiceRevisions`、`listServiceRevisionAudit`：读取预授权槽位和服务组合记录；
- `previewPluginInstallation`、`previewSelfServicePluginInstallation`、`previewDevelopmentPluginInstallation`：入口分别固定控制器、服务自助和开发来源，再由统一工作流生成无副作用安装计划；服务自助只接受 Portal BFF，目标在 P4 必须由 ManagementTarget 派生；
- `create/list/get/submit/approve/activate/cancel/rollbackPluginInstallationCandidate`：持久化安装候选并复用现有 ServiceRevision 审批、发布和单调回滚；创建与草稿原子落盘，取消只允许 Draft；
- `applyDevelopmentPluginInstallation`：只接受签名 `platform-dev/installation-watch` system caller，把一个已绑定的源码 Install/Upgrade/Remove 依次提交、按策略批准并进入同一 Generation 激活链；
- `create/update/submitServiceDraft`：由 `platform.deployment.compose` 编辑并提交仅含应用插件的组合；
- `create/update/refreshIntentDraft`：保存 Application Intent、调用 Planner，并显式接受发生变化的计划快照；旧 Composition 草稿操作只在 P4 切换 Portal 前供现有页面使用，不是长期兼容协议；
- `approveServiceRevision`：由不同的 `platform.deployment.approve` 用户批准；
- `publish/rollbackServiceRevision`：由 `platform.deployment.publish` 通过可信内核发布或创建新 revision 回滚。
- `create/get/publishConfigurationActivation`：仅接受 plugin-settings 精确身份；从当前活动目录重建 Application 配置修订，禁止普通发布入口绕过候选凭证窗口，并在 readiness 失败时自动发布单调回滚。
- `list/putTestTargetBinding`：读取或由 `platform.deployment.test-target` 以 CAS 预授权 Backend 应用插件测试槽位；
- `list/createTestRelease`：读取记录，或由 `platform.deployment.publish` 提交精确 testing 制品并等待候选结果；
- `rollbackTestRelease`：恢复回滚被控制器重启中断且已标记 `rollbackRequired` 的发布。

所有人员操作已进入插件签名 Manifest 的 `authorization` 目录。服务编辑、审批、发布/回滚和测试目标授权保持分离；Manifest 的 `different-subject` 只提供策略元数据，提交人与审批人分离仍由本服务的持久状态机最终强制。`bindIntentConfiguration` 不是人员操作，只接受精确 plugin-settings workload，并且只携带 CredentialRef。面向浏览器的服务修订输出会删除可信 Configuration Snapshot，并统一裁剪已编译提案中的 `managed_credentials`。

0.19.0 完成 ADR-0143 P3：Intent revision 持久化完整 Resolution Report，提交、审批和发布前分别重新调用 Planner；`planDigest`、活动 Platform Profile、Catalog revision 或配置摘要变化会把 revision 退回 Draft、标记 `planningStale` 并撤销已提交/已审批摘要，只有显式刷新后才能重新进入审批。`kernel.deployment.targets` 只向认证 Deployment Manager 返回内部 Planning Profile，公开 `listDeploymentTargets` 仍只暴露 Profile 摘要；最终发布继续走原有 `kernel.deployment.preview/publish` 强制点。

0.20.0 完成 ADR-0143 P4：Portal 改为 Workbench Application Intent 表单，移除依赖、实例策略、状态模型、逻辑服务和路由等内部执行输入，增加 Feature、插件配置及受限容量/放置意图；Resolution Report、服务依赖图、Provider Binding、配置计划、Artifact Lock、诊断和内核 Deployment 只读展示。在线 BFF、SDK、Descriptor 与管理绑定不再暴露 `createServiceDraft/updateServiceDraft`，历史 Composition revision 仅保留只读查看。

0.21.0 把根插件输入收窄为“固定版本/兼容升级”两种策略，提交共享 `ArtifactRequirement`，并继续只展示 Planner 生成的精确 Artifact Lock。复杂 SemVer 表达式不进入普通管理表单。

0.21.1 适配精确制品摘要贯穿契约：Planner 输出的 `id + version + channel + sha256` 作为部署候选原样提交，运行阶段不再重新求解版本。

0.22.0 完成 ADR-0191 P1：统一安装 Preview 契约位于中立 Library，Deployment Manager 只负责把安装/升级/卸载投影为候选 Application Intent 并编排既有 Planner/Kernel Preview。精确依赖、Catalog revision、配置缺口、Profile 和摘要均为只读结果；预览不写共享账本。服务市场、跨服务控制器和开发 watcher 后续只能作为该协议的来源适配器，不能建立平行安装链。

0.23.0 完成 ADR-0191 P2：Installation Candidate 记录来源、目标、变更、预览摘要和关联修订，但不复制 ServiceRevision 状态机。查询状态实时投影提交、审批、发布、stale、活动和回滚事实；取消原子删除未提交草稿并保留审计记录。插件安装新增申请、审批、激活三类独立权限，预览权限不能写入或发布。

0.26.0 完成 ADR-0191 P5—P6：开发目录只通过显式 TestTargetBinding 形成 Installation Intent；`allowInstall` 控制是否允许从空槽位安装，`portalTargets` 精确限制前端影响面。全栈候选按“全部 Portal 预热 → Backend 发布 → Portal 提交”执行；Portal 提交失败会逆序补偿 Portal 并发布单调 Backend 回滚。来源 target 和 Portal 集合都在可信入口固定，空集合明确表示 Backend-only。

ADR-0143 P5 的真实签名插件测试进一步验证了本插件的状态机边界：仓库解析结果变化会在审批时持久化 `planningStale`、退回 Draft 并撤销摘要；显式刷新和重新审批后，发布仍只经既有可信内核服务完成。该验收未给 Deployment Manager 增加仓库私钥、制品读取、Deployment CAS 或 Node Agent 控制权。

活动作业期间节点定义被冻结。新执行实例首次读取某租户共享账本时，未确认的 `Connecting/Installing` 会落为 `Failed` 且不自动重放，避免高权限 SSH 被重复执行。服务发布采用不同语义：`Publishing` 可用同 revision/同摘要幂等重试；中断的 Test Release 则 fail-closed 并要求显式恢复回滚。`kernel.deployment.targets/preview/publish/readiness` 只接受精确插件身份，并由内核固定 Profile、验签制品、CAS 写入和判断真实收敛。租户状态使用 `tenant/deployment.control/tenant` 单文档 CAS 聚合；Store revision 可拒绝 stale writer，但不等同于外部 SSH/systemd 副作用 fencing，因此当前保持单 Leader。运行说明见插件目录 [README](../../../extensions/plugins/cn.vastplan.platform.infrastructure.deployment-manager/README.md)，完整边界见《[服务部署控制台](../architecture/服务部署控制台.md)》、《[制品仓库与测试发布](../architecture/制品仓库与测试发布.md)》、[ADR-0070](../decisions/ADR-0070-Deployment-Manager与可信引导执行边界.md)、[ADR-0071](../decisions/ADR-0071-签名Node-Lease与可信就绪判定.md)、[ADR-0077](../decisions/ADR-0077-Backend在线组合与可信发布边界.md)、[ADR-0097](../decisions/ADR-0097-测试制品仓库与前端分级热升级.md) 与 [ADR-0126](../decisions/ADR-0126-Deployment-Manager共享账本与副作用Fencing.md)。

0.18.0 起，mutating 内核回调必须携带 Runtime Host 注入的当前 Unit Leadership evidence；插件不可读取或提交 epoch/token。SSH 引导以持久 Job ID 作为 OperationID，远端使用 root-owned fence 目录、`flock` 和单调 epoch 拒绝旧 leader；Deployment 与 Platform Profile 写入继续叠加既有 revision/request digest CAS。完整设计见 [ADR-0128](../decisions/ADR-0128-统一Leader-Epoch与外部副作用Fencing.md)。

0.5.0 起，服务发布在内核切换前先提交“旧活动 + 新候选”引用并集，切换成功后先固化回滚引用、再收敛活动引用；任一步失败只会多保护对象。精确引用同步由持久化 `referencePending` outbox 驱动，仓库恢复后在管理读取路径自动幂等重试，控制器重启也会重新校验活动 revision。Backend Test Release 在候选激活前还会发布独立的精确 artifact-lock owner。仓库不可用时候选 fail-closed，GC 不会获得一个缺引用但看似健康的窗口。

ADR-0145 P3 起，Backend Test Release 只接收完整 Repository Receipt。Controller 不再信任调用方分别提供的 ref、SHA 和 revision，而是通过仓库能力按当前 Profile 复核 repository ID、protocol、Profile digest、精确 Catalog 事实以及可选 workspace lease；引用保护仍先于候选激活，因此 workspace TTL 清理不会误删正在测试的候选。

0.6.0 起，部署预览由可信内核返回跨 Seed、托管仓库等来源解析后的精确制品引用；Deployment Manager 只消费并持久化该结果，不再旁路查询某一个仓库。这样引用保护与实际部署解析使用同一份事实，也避免 Seed 基础插件被误判为托管仓库缺失。

0.7.0 起，服务组合页面完全使用 Workbench Collection、动态 Form 与 Overlay 契约；部署目标枚举只在抽屉打开时加载，编辑和生命周期动作按所选 revision 状态显示，最终预览与审计不再由功能插件直接拼装 UI。

0.10.0 起，可信内核预览同时返回 `ConfigurationCatalog v1`，Deployment Manager 将其与服务 revision 一同持久化用于审计。0.11.0 起，目录读取统一收口到内核控制面 sidecar：初始启动和在线发布使用同一路径，只有与活动 Deployment revision/digest 匹配的目录可见；Deployment Manager 不再提供一个覆盖不完整的平行目录查询接口。
