# ADR-0077：Backend 在线组合与可信发布边界

- 状态：已接受
- 日期：2026-07-19
- 扩展：ADR-0057 的 Backend 双输入组合、ADR-0068 的平台管理强类型 BFF

## 背景

平台管理中心需要在线配置多个 Backend 服务、应用插件、实例数、依赖和调度规则，并把同一服务扩展为多个副本。若浏览器或 Deployment Manager 能提交 Platform Profile、完整 Deployment v2、KV key 或路由目标，应用管理员就能替换基础插件、绕过插件分级和向其他租户发布。若每个部署再实现一套控制器，又会与现有 Resolver、Deployment v2、Controller 和 Node Agent 产生双重真相源。

## 决策

1. 平台运维发布不可变 `BackendPlatformCatalog`。Catalog 保存 Platform Profile，并以精确 `(tenantId, deploymentName)` 绑定预授权部署目标；运行中的内核只在启动时解析一次，不接受插件或浏览器修改。
2. 在线管理唯一可写输入是 Backend `ApplicationComposition`。Deployment Manager 会用认证 tenant 和服务端 revision 重写其文档身份；应用管理员只能配置 application unit、应用插件、replicas、依赖、资源和 placement，不能提交 Platform Profile、平台插件、KV key、logical target 或 routing domain 选择器。
3. Backend Kernel 提供三个只向精确 Deployment Manager 插件开放的窄服务：`kernel.deployment.targets`、`kernel.deployment.preview`、`kernel.deployment.publish`。插件不得取得 Catalog、制品仓库凭证、信任根、NATS 连接或 KV 句柄。
4. 预览和发布都由内核重新选择 Catalog 中的精确 Profile，复用现有 Composition Resolver 与 Node Agent 同源的制品读取/验签链。具体 Resolver 只在 Backend 组合根通过窄函数接口注入发布器，发布器不得横向依赖同级实现包。发布必须携带已审批的预览摘要；摘要变化则拒绝，要求重新预览与审批。
5. 发布结果仍是唯一的 Deployment v2，以 tenant/name 派生 KV key，并通过已有单调业务 revision 与 KV CAS 写入。Controller fleet 对 Catalog 中全部预授权目标分别选主和 watch；每个目标可有独立 Node Agent 集合和任意允许的副本数。
6. 服务组合状态机为 `Draft → PendingApproval → Approved → Publishing → Published`。提交人与审批人必须不同；compose、approve、publish 角色分离。回滚复制历史应用组合并生成新的单调 revision，不回退 KV revision。
7. `Publishing` 是可恢复状态。若发布已写入 KV、但插件在最终状态落盘前退出，操作者重试相同 revision 与摘要；控制面的同 revision 同内容写入是幂等的。插件或门户不可用不会停止已发布服务，Controller 和 Node Agent 继续按最后期望态运行。
8. Deployment Manager 的前端模块仍按 ADR-0073/0076 进入 Portal 内容寻址快照。单门户可绑定多个部署服务；多个门户可显式共享同一部署服务，看到同一租户隔离的审批账本，但权限由各自 PortalBinding operation grant 独立约束。

## 否决方案

- 浏览器直接编辑完整 Deployment v2：暴露平台插件、来源锁和控制面目标，破坏双输入授权边界。
- 把 Platform Profile 存进 Deployment Manager：基础插件随应用状态漂移，插件故障可能改变平台基线。
- 发布时只信任草稿预览：Profile 或制品状态变化会产生“批准内容”和“实际发布内容”不一致。
- 一个部署启动一个自定义编排器：重复 Controller、调度和故障恢复模型，长期不可治理。
- 让一个 Node Agent 同时消费多个 Deployment assignment：扩大单进程故障域并破坏现有 Node Lease 的精确 deployment 身份；需要同机承载多个目标时运行多个 systemd Node Agent 实例。

## 结果

平台管理中心获得完整在线服务组合能力，同时 Backend 微内核仍只拥有 Catalog 选择、可信解析和 CAS 发布等通用治理能力。代价是生产部署必须预先发布 Catalog，并为每个目标运行 Controller fleet 和带精确 deployment 身份的 Node Agent；新增部署槽位属于平台运维变更，不是应用管理员操作。
