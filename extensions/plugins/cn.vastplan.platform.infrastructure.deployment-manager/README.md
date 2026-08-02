# VastPlan Deployment Manager

`cn.vastplan.platform.infrastructure.deployment-manager` 是平台基础插件，负责：

- 保存租户隔离的 Linux 节点定义；
- 以 CAS 管理节点计划版本；
- 管理 `Pending → Approved → Connecting → SystemdActive → Ready/Failed` 首次引导作业；
- 强制申请人与审批人分离；
- 重启时把未确认的 `Connecting/Installing` 作业标记为 `Failed`，禁止自动重复执行；
- 向可信宿主提交类型化 `kernel.node.bootstrap` 请求；
- 通过窄化的 `kernel.node.readiness` 观察签名 Node Lease，并在引导完成或查询作业时收敛最终状态。
- 列出平台预授权的 Backend 部署目标；
- 管理 Application Intent、Resolution Report、异人审批、发布审计和单调 revision 回滚；
- 通过统一 `PluginInstallationIntent` 协议为平台控制器、服务自助和开发自动化生成无副作用的应用插件安装、升级、卸载预览；
- 提供跨服务“服务插件”页面，把批量目标拆成独立候选，并在用户刷新时聚合内核 readiness 副本进度；
- 为 plugin-settings 创建 candidate 绑定的 Application 配置修订，禁止普通发布入口绕过候选凭证准备，并在 readiness 失败时自动发布回滚 revision；
- 为 Platform Profile restart 配置持久化独立 Saga：调用窄内核端口准备候选、执行异人审批、激活 Catalog、发布 Deployment、等待精确 readiness，并在失败时依次生成单调 Catalog/Deployment 回滚修订；
- 通过 `kernel.deployment.preview/publish` 请求可信内核选择固定 Platform Profile、验签制品并 CAS 发布 Deployment v2；
- 以 CAS 保存 Backend 应用插件的 `TestTargetBinding`，精确复核 testing Catalog ref、摘要、publisher 与 repository revision；
- 通过 `kernel.deployment.readiness` 等待候选修订真实收敛，失败时以新的单调 revision 自动恢复上一组合；
- 重启时把未完成测试发布落为 `Failed + rollbackRequired`，由 `rollbackTestRelease` 显式、安全地完成恢复，不盲目重放；
- 提供 Portal 动态表单页面，配置应用插件、服务依赖、replicas、实例策略和节点标签，并展示最终解析预览。

插件只保存 Credential 名称，不保存、读取或返回 SSH 私钥、known_hosts、NATS 身份、制品令牌等 material。`kernel.node.bootstrap` 只由 Backend Kernel 注册，负责通过 CredentialBroker 使用凭证并执行固定 Linux/SSH/systemd Provider。

`kernel.node.readiness` 也只由 Backend Kernel 注册。内核校验预期 tenant、Deployment、cluster-global node ID、transport 公钥、Lease 签名、KV key 与新鲜度，插件只接收 `Waiting/Ready/Rejected`，不获得 NATS 连接、KV 句柄或 transport trust。观察服务暂时不可用时作业保持 `SystemdActive`；身份或签名不匹配进入 `Failed/readiness_rejected`，超过期限进入 `Failed/readiness_timeout`。

0.19.0 起，在线服务组合以 Application Intent 为用户输入，并持久化 Planner 返回的 Resolution Report。提交、审批、发布前都重新规划；任一计划输入变化都会撤销审批并标记 stale，不能沿用旧摘要发布。`BackendPlatformCatalog` 的启动文件只是可信 Seed，以 `(tenantId, deploymentName)` 锁定精确 Platform Profile；内核只通过认证的 targets 回调向本插件提供规划所需 Profile，浏览器仍只能看到摘要。真正的 Profile 克隆、Catalog CAS、Composition 复验和 Deployment 发布仍在内核。Profile 候选存在时目标 binding 的普通 Application 发布被锁定；中断依靠 candidate/request digest 检查点恢复，不盲目重放副作用。

0.20.0 起，Portal 只允许编辑 Application Intent：根插件、Feature、非敏感插件配置和受限容量/放置参数。依赖、实例策略、状态模型、逻辑服务、路由、Provider Binding 与精确制品锁全部由 Planner 派生并只读展示；旧 Composition 创建/更新操作已退出在线 API，历史 revision 只用于审计和查看。

0.21.0 起，根插件版本采用固定 `=x.y.z` 或兼容 `^x.y.z` 两种用户策略；运行、审批、回滚和节点安装仍只消费 Planner 产生的精确锁。

0.21.1 起，精确锁同时包含制品 SHA-256；部署管理只转交和审计该锁，不在运行阶段重新解析版本。

0.22.0 起新增统一安装预览：控制器、服务自助和开发自动化分别使用受治理 operation，在入口固定 `controller/self-service/development` 策略对象后调用同一工作流，来源不能由请求体自报。服务自助入口只接受 Portal BFF，后续 BFF 必须从 ManagementTarget 派生 deployment；开发来源只接受 `platform-dev` 系统身份并要求命中现有 TestTargetBinding。工作流把一个根插件变更投影到活动 Application Intent，复用 Planner 和 Kernel Preview，返回精确依赖差异、配置缺口、Catalog revision、plan/preview digest 与 Service Generation 影响，但不会创建 revision、下载、保护引用或激活制品。Foundation/Platform 插件在该入口 fail-closed。

0.23.0 起增加 Installation Candidate 持久生命周期。创建候选与 ServiceRevision Draft 原子提交；`list/get/submit/approve/activate/cancel/rollbackPluginInstallationCandidate` 只编排既有服务修订。候选状态由修订投影，提交和审批仍会重新规划并检查 stale，激活和回滚仍经可信内核。关联修订拒绝通用编辑、提交、审批和发布入口，避免绕过安装权限；取消只允许未提交草稿并保留候选审计。完整 Artifact Lock 只保存在关联 ServiceRevision，候选记录不重复占用 Shared State 容量。

0.24.0 起提供跨服务控制器页面和固定 Portal BFF 路由。浏览器只能提交逻辑 deployment/unit 的变更意图，不能选择来源、capability、logical service 或物理节点；批量目标按 Deployment 生成独立候选，同一 Deployment 同时只允许一个未完成候选。页面的预览、申请、审批和激活操作使用独立权限。候选列表仅在用户刷新时读取 `kernel.deployment.readiness`，展示期望、已观察和就绪副本；该观察不写入候选账本，也不引入后台轮询。

0.25.0 起增加服务自助适配器。Portal BFF 从已发布 `ManagementTarget.resource` 派生唯一 backend deployment/unit，浏览器只提交插件变更；候选查询和所有生命周期 operation 再次校验 `self-service` 来源与相同目标。可选 `approval.policy.v2` Binding 在组合根注入：策略允许时提交后由策略主体进入既有 `Approved` 状态，需要证据时保留 `PendingApproval`，拒绝时不创建候选。控制器、自助和开发入口仍复用同一个候选与 ServiceRevision 状态机。

测试目标绑定只能指向活动 Application Composition 内已有的应用插件，不能增加插件，也不能覆盖 `cn.vastplan.foundation.*` 或 `cn.vastplan.platform.*`。测试发布只接受 `testing` channel 的 SemVer 预发布版本和精确 SHA/repositoryRevision；上传与发布是两个事务。候选就绪与回滚通过 `kernel.deployment.readiness` 读取内核持有的 NATS Composition report，插件不获得 KV 句柄。

0.17.0 起，租户状态通过可信宿主 `kernel.state.shared.get/create/update` 保存为 `tenant/deployment.control/tenant` 单文档 CAS 聚合；插件不持有 NATS、数据库或文件系统凭证。0.18.0 进一步复用 Unit Leadership 的 host-only epoch/token 保护 mutating 内核回调：新 leader 可以读取同一账本并执行中断恢复，旧 leader 的过期写入被 Store revision 拒绝，失效 Runtime 不能开始新副作用；SSH 远端再用 root-owned `flock` 与单调 epoch 拒绝延迟旧请求。当前仍采用 `leader + external-shared + leader routing`，不宣称 active-active。

运行该插件的管理节点必须使用与自身作用域绑定的 `manager-node` NATS 身份，并配置 `-tenant`、`-deployment`、`-node-id`、`-transport-seed` 与 `-transport-trust`。生产在线编排还必须配置 `-backend-platform-catalog /etc/vastplan/backend-platform-catalog.json`，并给该可信内核身份最小 Deployment KV 写权；Controller 进程使用同一 Catalog 的 `controlplane -controller -backend-platform-catalog ...` 为全部目标调度。它还必须配置 `-credential-root /secure/vastplan-credentials`；目录布局固定为 `<root>/<tenant>/<credential-name>`，material 文件必须为 `0600`。缺少任一依赖时相应内核服务不注册并 fail-closed。
