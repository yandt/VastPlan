# ADR-0116 Backend Platform Profile 候选 Catalog 与配置激活

- 状态：已采纳，已实现
- 日期：2026-07-23
- 关联：[ADR-0057](ADR-0057-插件分级管理与双输入组合解析.md)、[ADR-0077](ADR-0077-Backend在线组合与可信发布边界.md)、[ADR-0113](ADR-0113-可信插件配置目录与分路径生效.md)、[ADR-0115](ADR-0115-Application配置激活Saga与候选凭证窗口.md)

## 背景

Application 配置可以生成新 Application Composition，但来自 `platform-profile` 的 `service + restart` 配置必须修改 Backend Platform Profile。现有 `BackendPlatformCatalog` 在内核启动时只解析一次，这保证了应用管理员无法替换平台基线，但也无法支持受治理的在线 Profile 配置。

一份 Profile 可被多个 tenant/deployment 引用。若原地改写，单租户配置会意外影响其他租户；若只发布新 Deployment 而不更新 Catalog，之后的 Application 发布又会使用旧 Profile。Catalog、Deployment、候选凭证和 readiness 因此必须纳入同一可恢复 Saga。

## 决策

### 1. 不新增插件，但在 Deployment Manager 内部分离 Profile Activation 模块

Deployment Manager 继续持有服务修订、异人审批、readiness 与单调回滚账本；新 Profile Activation 以独立文件、状态和内部 operation 实现，不把 Catalog 管理混入普通 Application 编辑流。plugin-settings 只能请求精确配置候选，不能提交 Profile、Catalog、binding 或受影响目标列表。

本控制路径继续使用 Go：它位于 Resolver、NATS CAS、Deployment 发布和 Go 持久状态机的交叉处。Node/Python 不会带来驱动或生态优势。运行方式仍是 Deployment Manager 首方可信独立进程，不新增后台进程。

### 2. 按精确 binding 克隆 Profile revision，不原地修改共享 Profile

配置资源来自当前活动 `(tenant, deployment, unit, plugin)` 目录。内核从当前 Catalog 解析绑定的精确 Profile，只修改该 Profile 中匹配的独立平台服务配置，生成新的单调 Profile revision 和 Catalog revision，并仅把目标 binding 指向新引用。其他绑定继续引用旧 Profile，因而不发生跨租户隐式变更。

Profile `attachments` 目前不携带独立 config，所以通用编辑第一阶段只处理 Profile `services` 中的独立服务。将平台插件附着到 Application unit 但又允许应用配置它，会重新打开越权边界，不作隐式支持。

### 3. Catalog 改为可信 Snapshot 端口，活动与候选都在内核控制面

`deploymentpublisher` 不再持有可变 Catalog 值，而是每次 Targets/Preview/Publish 从窄 `CatalogSource.Snapshot` 取得并严格复核快照。启动文件继续作为 Seed；在线模式后续由 NATS KV 的版本化 Catalog Store 提供快照。Deployment Manager 始终只调用窄内核服务，不取得 NATS、KV、Catalog 全文或信任根。

Catalog Store 持久保存：当前活动 Catalog、单个候选 ID、期望旧摘要、目标 binding、新 Profile/Catalog 摘要及状态。候选存在时，普通 Application 发布必须拒绝同一 binding，防止 Profile 回滚覆盖并发的应用变更。

### 4. 发布顺序与补偿固定

```text
Draft -> PendingApproval -> Approved
  -> CandidateCredentials
  -> ActivateCatalogCandidate
  -> PublishDeploymentRevision
  -> WaitExactReadiness
  -> FinalizeCatalog + ActiveCredentials -> Ready
```

内核先用 Catalog CAS 激活候选 binding，再发布由该候选 Profile 解析的精确 Deployment revision。中断后依靠 candidate ID 和完整请求摘要幂等继续。readiness 失败时，先生成指回旧 Profile 的新单调 Catalog revision，再以新 Deployment revision 发布旧 Application + 旧 Profile 解析结果，最后 Abort Candidate 凭证。不允许倒退 Catalog revision、Deployment revision 或 KV revision。

如果 Catalog 已激活而 Deployment 尚未发布，已运行实例仍按旧 Deployment 工作；新 Application 发布又被 binding lock 拒绝，所以不会产生无法判定的中间组合。

### 5. 权限不复用 Application 发布授权

Profile 候选提交和发布使用独立的 `platform.plugin-configuration.profile.publish` 权限，审批继续要求不同主体。只有精确 plugin-settings 身份能请求 Profile Activation，只有精确 Deployment Manager 身份能调用内核候选 Catalog 端口。浏览器不获得 Profile/Catalog 全文、NATS key、凭证 handle 或内部锁。

## 备选方案

- **plugin-settings 直接保存 Profile/Catalog**：会绕过内核的来源锁、Resolver 和制品验签，否决。
- **独立新建 Platform Profile Controller 插件**：边界清晰，但会复制 Deployment Manager 的审批、修订、readiness 和回滚账本；当前选择同插件内部模块化。
- **原地改写共享 Profile**：会使单一绑定的配置扩散到其他租户/部署，否决。
- **只发布 Deployment，不更新 Catalog**：下次 Application 发布会恢复旧 Profile，产生双真相源，否决。
- **同时原子写 Catalog 与 Deployment 两个 KV**：JetStream KV 不提供跨 key 事务；伪装原子性会隐藏必须实现的恢复和补偿，否决。

## 实施阶段

1. 将 `deploymentpublisher` 改为严格 `CatalogSource.Snapshot` 端口，保留静态 Seed 适配器。
2. 增加 NATS Catalog Store、候选锁与精确 Deployment Manager 内核服务。
3. 增加 Deployment Manager Profile Activation 状态和 plugin-settings 适配器。
4. 接入独立权限、BFF/SDK/Workbench，完成重启恢复、并发、失败回滚和真实多服务验证。

## 影响

- ADR-0077 的“运行内核只在启动时解析 Catalog”改为“启动文件是 Seed，内核每次使用可信活动 Snapshot”；“插件和浏览器不能修改 Catalog”仍保持。
- 任一已发布服务仍不依赖管理面存活；候选只阻塞目标 binding 的新变更。
- Catalog Store 和生产 NATS ACL 必须有独立版本与最小写入权限，不得依赖本地 insecure NATS 权限验证。

## 实施记录（2026-07-23）

- 第 1 阶段已完成：`deploymentpublisher` 通过 `CatalogSource.Snapshot` 逐次读取、严格复核并使用 Catalog；静态启动 Catalog 仅是同端口的 Seed 适配器。
- 第 2 阶段的读取基座已完成：新增 `VASTPLAN_BACKEND_PLATFORM_CATALOGS_V1` 独立 KV，保留 64 代历史；`platformcatalog.Store` 实现严格快照、摘要和身份复核，持久数据损坏时不得回退 Seed。Bootstrap/Controlplane 可 create-only 发布 Seed，Manager Node 只读且 NATS ACL 明确拒绝写入。
- 第 2 阶段的写入基座已完成：新增绑定精确 Catalog ID、只能读写对应确定性 key 的 `catalog-publisher` NKey 角色；承载 Deployment Manager 的可信内核以第二条连接持有该 object capability，Manager 连接仍只读。候选状态与活动 Catalog 在同一 value 中 CAS，覆盖 Prepare/Activate/Finalize/Abort/Rollback、精确请求幂等、并发单赢家、目标 binding 发布锁和单调回滚；普通 Application 预览/发布同时锁定 Catalog digest 与 Profile ref。
- 当时待续：精确 Deployment Manager Profile Activation 内核端口与可恢复 Saga；完成情况见下一条记录。
- 第 3、4 阶段随后完成：新增不返回 Profile/Catalog 全文的 `kernel.platform-profile.*` 窄端口，内核从活动 ConfigurationCatalog 与目标 binding 重建精确 service 配置、选择高于同 ID 全部历史的 Profile revision，并区分 Prepared 预览与 Activated 发布。deployment-manager 0.16.0 持久化独立 Profile Activation 记录、请求摘要、预览、异人审批和恢复检查点；成功路径固定为 Catalog activate → Deployment publish → readiness → finalize，失败路径固定为单调 Catalog rollback → 新 Deployment rollback。普通 Application 发布在同 binding 候选期间被本地账本和 Catalog Store 双重阻断。
- plugin-settings 0.7.0、Node BFF、TypeScript SDK 与 Workbench 已接入独立 `platform.plugin-configuration.profile.publish` 权限，提供提交、审批、激活和放弃动作；候选凭证继续只以 managed ref 进入 Candidate 窗口，公开状态不含 handle、stage ID 或请求摘要。单元与纵向测试覆盖精确 caller、跨租户拒绝、attachment 拒绝、制品漂移、重启恢复、激活前发布拒绝、readiness 失败双重回滚和用户直达拒绝。真实多服务启动验收在本次实施记录之后执行并记录结果。
- 真实多服务启动验收已完成：清理仅限本地开发的旧状态后，平台 Backend revision 21 的 11 个 active unit 与受管服务 revision 2 的 1 个 unit 均收敛 `Ready`；deployment-manager 0.16.0、plugin-settings 0.7.0 和 platform-admin-access-policy 0.23.0 均以目标版本激活，Portal `/operations` 实测 HTTP 200。测试结束后由开发编排器优雅停止全部受管进程。旧开发状态因格式 4→5、1→2 且无 lifecycle migration 被拒绝符合 fail-closed 预期；生产升级不得依赖清理，必须在形成历史数据前单独补充迁移策略。
