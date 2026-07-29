# ADR-0169 Seed Recovery Capsule 与分阶段可用性

- 状态：已采纳，已实现
- 日期：2026-07-29
- 关联：[ADR-0049](ADR-0049-制品信任基座与仓库基础插件.md)、[ADR-0094](ADR-0094-操作系统Guardian与独立进程故障收敛.md)、[ADR-0101](ADR-0101-离线Bootstrap-Inventory与LKG推进.md)、[ADR-0142](ADR-0142-内核启动与业务发布完全分离.md)、[ADR-0157](ADR-0157-开发Seed-Runtime-LKG快照.md)

## 背景

平台管理 Seed 需要在远端仓库、在线控制面或部分平台插件不可用时恢复。如果把仓库、权限、配置和部署服务直接编译进 Backend，虽然减少启动依赖，却会扩大内核故障域、强制内核随插件升级，并重新形成单体系统。若所有 Seed 插件都等价对待，任一非关键服务失败又会把“系统不可用”和“部分能力不可用”混为一谈。

现有 Bootstrap Inventory、LKG、Seed Runtime Snapshot、Runtime Manager 和 Process Guardian 已具备恢复基础，但最小 LKG 插件集合仍由开发工具中的 ID 白名单隐式决定，平台启动也只有整体收敛门禁，无法表达恢复面、控制面和完整平台的分阶段可用性。

## 决策

采用“不可插件化 Seed Core + 隔离运行的 Seed Recovery Capsule + 普通平台插件”三层模型，不把具体插件业务代码编译进内核。

### 1. Seed Core 只保留机制

内核永久保留制品与发布者验证、本地 Seed Source、内容寻址缓存、安装事务、LKG、Runtime Manager、Process Guardian、强制授权挂载点、无策略时默认拒绝和最小诊断/恢复入口。存储、仓库 API、角色策略、凭证、数据库、部署规划及管理 UI 均不进入内核。

### 2. Recovery Capsule 是精确恢复闭包

`Recovery Capsule v1` 同时绑定：

- Bootstrap Inventory 的 `repositoryId + generation`；
- LKG 中每个插件的精确 `pluginId + version + channel + sha256`；
- 每个启用 Seed service unit 的唯一可用性阶段及 `minReady` 最低健康副本数。

Capsule 不是新的制品仓库、Deployment 输入或发布器。它只描述已发布 Seed/LKG 的恢复闭包与观测分级，不复制在线配置真相源。LKG 直接从 `recovery` 阶段 unit 的插件闭包派生，不再维护第二份插件 ID 白名单。新增、删除或重命名 Seed unit 后未同步分级必须 fail-closed。

### 3. 三阶段累计就绪

- `Recovery Ready`：本地 Seed 和托管仓库恢复能力可用，足以继续取得已验证制品或进入恢复流程；
- `Control Plane Ready`：在 Recovery 基础上，设置、凭证、授权、部署、组合规划和 Portal 管理控制面可用；
- `Platform Ready`：在 Control Plane 基础上，数据库、API 暴露、认证投递等当前平台扩展能力全部可用。

后续阶段累计包含前序阶段。后序失败不得把已经成立的前序阶段改报为不可用。普通 `up/restart` 只恢复最后提交状态并持续观测，不因生成 Capsule 而执行 Deployment、Portal Activation 或业务发布。显式 `bootstrap/apply` 可按阶段等待：Recovery 成立后即可启动最小 Portal，Control Plane 成立后才执行显式 Portal 发布，Platform 成立后才提交新的开发 Seed Runtime LKG。

### 4. 运行与升级边界

Capsule 插件随正式发行的签名 Seed Bundle 提供，但仍由独立进程或可信 Runtime Host 承载，并受 Guardian、资源限制、健康检查和 generation 切换约束。插件崩溃不得拖垮 Kernel；候选只有在恢复阶段健康后才可推进 LKG，旧 LKG 继续保留。dynamic-go 也只进入独立 generation-scoped Go Runtime Host，不回到 Backend 进程。

### 5. 状态、集群聚合与降级

状态聚合只读取当前 Node Agent 更新且未过期的 ActualState，拒绝上次进程遗留的 Ready。每阶段报告 `Pending / Ready / Degraded / Failed`，按 unit 汇总达到 `minReady` 的副本；平台总体状态报告当前最高成立阶段。每个节点把不含路径、错误正文、PID 或秘密的有界 `NodeReport` 写入传输身份签名的 Node Lease v4。集群聚合只接受同 tenant/deployment、同 Capsule/Inventory generation、签名有效且 45 秒内新鲜的报告。

NATS 或集群目录不可用时，Kernel 保留本机报告并显式返回 `scope=local, clusterAvailable=false`，不得把单节点状态冒充集群状态；后续 Control Plane/Platform 失败也不得抹掉已经成立的 Recovery Ready。开发网关通过 `recoveryCapsule` 状态公开同一共享契约，不另写判断分支。

### 6. systemd、只读端点与最小页面

配置 Capsule 的 Backend Kernel 把状态原子写入 owner-only 文件，并通过 `sd_notify STATUS=` 持续投影阶段；原有 `READY=1` 仍只表示 Node Agent 已接管控制面，不能冒充 Platform Ready。可选 `-recovery-listen` 只允许 loopback，提供 `/healthz`、Recovery `/readyz` 和 `/v1/recovery/status`，不提供发布、修复或任意运维动作。

Portal Host 只连接显式 loopback `--kernel-recovery-url`，在公共 `/v1/kernel-recovery` 删除 Capsule、仓库、generation 和 unit issue 等内部身份，仅保留阶段、计数、范围和集群可用性。固定 `/recovery` 是无脚本、无设计系统、无 Workbench、无插件模块的服务端 HTML；正常 Portal 启动失败页也消费相同裁剪状态。

### 7. 发行与升级

Linux SSH/systemd Release 可携带 HTTPS + SHA-256 锁定的可选 Recovery Capsule。固定引导脚本下载复验后以 `root:vastplan 0440` 安装，systemd 只把规范路径和 loopback 端点交给 Kernel。Capsule 已由外层不可变发行摘要保护；若同机同时提供 Bootstrap Inventory，Kernel 还会逐项复核 repository/generation 和 LKG 精确摘要。

生产 Recovery 节点禁止只推进 Bootstrap LKG 而没有配对 Capsule。恢复闭包升级必须作为“内核发行物 + 精确 Capsule”的签名发行事务，经 systemd 原子切换；开发 insecure 模式仍可测试 Bootstrap Upgrade，但不能作为生产旁路。

## 实施

1. 新增语言中立 `contracts/schemas/recovery/v1` 契约和严格解析、规范化、重复 unit/制品拒绝及累计阶段求值辅助函数。
2. `engineering/deploy/platform-recovery-plan.json` 显式覆盖平台管理 Profile 的全部启用 unit。
3. `platformdev` 从 Recovery unit 的插件闭包生成 Bootstrap LKG，并生成与 Inventory generation、精确摘要绑定的 `recovery-capsule.json`。
4. 开发 Seed Runtime Snapshot 把 Capsule 纳入内容摘要和完整性验证；Capsule 与 Inventory/LKG 漂移时拒绝恢复。
5. 开发启动持续观察三阶段状态；显式发布按 Recovery、Control Plane、Platform 分段门禁，普通启动仍保持零发布。
6. Backend Recovery Controller、owner-only 状态文件、loopback HTTP、systemd STATUS、Node Lease v4 和按 unit `minReady` 的签名多节点聚合已接入。
7. Linux SSH/systemd 引导支持摘要锁定 Capsule；Portal Host 已提供裁剪 API、固定 `/recovery` 页面和启动失败状态面板。
8. 自动化故障矩阵覆盖过期/异代节点、签名验证门、集群不可用本地降级、后序失败保留 Recovery Ready、Capsule/LKG 漂移、文件权限、非 loopback 监听和支持包脱敏。
9. 开发编排器版本与 Seed Runtime LKG 解耦：普通启动复用快照内的精确插件仓库和 Runtime Host，只按源码摘要刷新与当前编排器配对的 Backend Kernel 宿主。刷新不重打 stable 插件包；只有整套服务健康后才把新宿主写入下一份 LKG。生产环境不使用该开发旁路，而由签名 Release Manifest 原子配对 Kernel、Runtime Host、Capsule 与插件闭包。
10. API Contract Catalog 是已验签 Seed 制品的确定性投影，不是发布状态。开发启动每次都从本次已验证制品重建该文件；文件丢失不得阻断独立的最小恢复页面，也不得触发 Deployment 或 Portal Activation。

## 影响

正面：关键恢复插件无需进入内核进程；LKG 与恢复 unit 的关系成为显式、可审计契约；平台可在部分故障时提供最小恢复面；新增 Seed 服务必须明确归类。

代价：发行物增加一个小型 Capsule 契约；阶段划分和 `minReady` 成为 Platform Profile 变更的必要同步项；Recovery 节点的生产 LKG 升级必须携带配对 Capsule，不能使用无配对元数据的普通自动推进。
