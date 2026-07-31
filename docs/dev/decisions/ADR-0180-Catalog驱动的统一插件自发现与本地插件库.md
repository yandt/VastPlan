# ADR-0180 Catalog 驱动的统一插件自发现与本地插件库

- 状态：已采纳，分阶段实施
- 日期：2026-08-01
- 关联：[ADR-0003](ADR-0003-插件装载模型.md)、[ADR-0098](ADR-0098-制品依赖解析锁与离线Bundle.md)、[ADR-0145](ADR-0145-本地测试与正式远端制品仓库双协议.md)、[ADR-0151](ADR-0151-最小Seed装配与开发插件候选分流.md)、[ADR-0179](ADR-0179-Ant-Design单实现与Renderer协议保留.md)

## 背景

VastPlan 已具备 Manifest 扫描、local-test/remote 双仓库协议、不可变制品、Catalog Journal、精确运行锁、Node Agent 安装验证和 Frontend Generation，但这些能力尚未形成一条覆盖 Backend、Frontend、Runner、Mobile 的统一插件发现链。开发源码增删由工程工具感知，Renderer/Shell 等 Foundation Library 仍存在构建期精确版本投影，local-test 仓库也还不能作为可由远端仓库扩充的本地插件库。

如果继续为 Renderer、Runtime Provider、数据库 Provider、认证协议等逐类维护候选清单，内核和工程工具会不断积累插件家族特例；如果让各内核直接扫描源码目录或远端仓库，又会复制版本求解、信任、缓存和恢复逻辑。

## 决策

### 1. Catalog 是运行发现的唯一事实源

所有普通插件先进入 **Local Plugin Library**，再由其不可变对象、Catalog Snapshot 和单调 Journal 驱动发现。开发源码、远端仓库和离线 Bundle 都只是输入适配器，不是内核运行事实源。

- 开发源码新增、修改和删除分别投影为 `published`、`updated`、`withdrawn` Catalog 事务；
- 远端安装先解析精确 Artifact Lock，再把包、原始发布证明和来源证据导入本地插件库；
- 事件只用于触发重读，消费者始终按 `repositoryId + revision + digest` 读取完整快照；
- 内核不扫描 `extensions/plugins`，也不根据远端目录自行选择 latest。

当前 `artifact.repository.local-test.v1` 的持久 File Volume、Catalog 和 Journal 演进为开发模式 Local Plugin Library。它继续允许本地 `workspace/testing` 发布，同时允许受信同步控制器导入远端精确制品；导入必须保留上游 publisher、channel、证明和摘要，本地导入回执不能替代上游身份。生产节点可以复用相同安装库/缓存格式，但正式远端仓库仍是发布与解析权威。

### 2. 发现、准入和激活分离

统一生命周期分为三个不同事实：

1. `Discovered`：Catalog 中存在可验证制品；
2. `Admitted`：发布者、目标内核、契约、策略和依赖满足当前环境要求；
3. `Activated`：具体 Platform Profile、Application Intent 或开发激活策略选择了精确制品并提交运行代。

开发环境可以注入 `DevelopmentActivationPolicy`，把 workspace 的新增、更新和撤销自动编译为候选 Generation；生产默认只自动发现，不自动启用。多个同 capability Provider 同时可用时必须由 Profile 绑定或显式策略选定，禁止按扫描顺序或发布时间静默切换。

### 3. Manifest Contribution Index 统一所有插件类型

已验证 Manifest 投影为语言中立的 `Plugin Inventory` 与 `Contribution Index`。索引至少包含精确制品身份、目标内核、扩展点、capability 提供/依赖、运行驱动、配置协议、贡献 kind、契约范围和依赖边。

Renderer、Shell Library、Runtime Provider、数据库 Provider、认证/授权协议、Storage Provider、API、页面以及未来贡献均使用同一索引。消费者按 `kind + owner/adapter/capability + contract compatibility` 查询候选，不得嵌入具体插件 ID 或生成精确版本源码表。未知贡献 kind 可以被 Catalog 展示，但只有存在兼容 Extension Point/Contract 时才可激活。

### 4. 统一调和，不统一热替换语义

四内核共享 `PluginInventorySnapshot`、`ContributionIndexSnapshot`、`ActivationPolicy` 和 `ReconciliationPlan` 契约，各自只实现运行产物适配：

- Backend 生成 Deployment/Assignment；
- Frontend 生成 Portal Activation/Generation/Host Epoch；
- Runner 生成签名 App Profile/运行锁；
- Mobile 生成受发布渠道约束的 App Profile/Bundle 计划。

所有插件都能在线发现和下载安装，不代表所有插件都可无刷新热替换。功能插件使用候选代切换；Renderer family、共享 ABI 或宿主变化使用 Host Epoch/受控刷新；有状态 Backend 使用 Drain/迁移/滚动升级；Kernel/Bootstrap 仍使用重启升级。

### 5. 撤销是事务，不是物理删除

源码目录消失或管理员卸载先产生 `withdrawn`，停止新的解析与选择。若插件正在运行，调和器生成更高运行代，先阻止新流量、检查强依赖、Drain、解绑贡献并停止实例。只有 Active、候选、回滚、Assignment、Seed、LKG 和租约引用全部释放后，GC 才能删除对象。

强依赖无法满足时必须保持旧安全运行代或令受影响单元 Blocked/NotReady，不能留下半卸载组合。撤销事件丢失时可由下一次完整 Catalog Snapshot 修复。

### 6. 内核边界

内核保留 Schema/签名/摘要和执行策略强制、原子安装、生命周期状态机、Runtime Driver、调度、路由、健康、Drain、回滚/LKG 与贡献注册原子切换。这些是不能委托给普通插件的可信强制点。

源码扫描、远端搜索、版本求解、仓库同步、市场排序、用户审批、Profile 编辑和业务发布属于插件或控制面工作流，不进入内核。仓库同步失败只阻止新安装，不影响已提交运行代；发现/规划插件失败只阻止新变更，不影响已经激活的数据面。

## 语言与运行形态

本地库同步、Catalog Journal 和调和控制使用 Go：可复用现有强类型仓库、原子文件、签名验证、NATS/KV 和控制器状态机，资源占用也适合常驻。它们优先作为现有仓库/组合管理插件中的独立领域模块运行，不为每类插件创建进程。Node.js 继续负责 Frontend 构建和浏览器运行投影；Python 不参与首个确定性目录事务，后续可作为搜索或分析 Provider。

## 备选方案

- **内核直接扫描目录**：开发简单，但目录无法表达远端来源、精确锁、集群一致性和安全撤销，否决。
- **每个内核直接订阅多个仓库**：灵活，但复制求解、凭证、缓存和恢复逻辑，否决。
- **Catalog 发现即自动启用**：体验直接，但上传或同步即可改变生产运行态，破坏审批和回滚边界，否决。
- **删除目录立即删除包体**：可能破坏活动实例、回滚和离线恢复，否决。

## 实施顺序

1. 定义统一 Inventory/Contribution/Activation/Reconciliation 契约并同步权威设计；
2. 实现 Remote → Local Plugin Library 的精确锁导入、上游证明保留和幂等 Journal；
3. 把工作区增删改映射为 local-test Catalog 事务与安全撤销；
4. 删除 Renderer/Shell 构建期精确版本表，改由 Contribution Index 生成候选；
5. 接入四内核运行产物适配器；
6. 用新增、更新、删除、远端安装、Provider 歧义、Drain、回退和重启恢复验证完整闭环。

## 影响

- 新插件类型只需声明 Manifest 与契约，不再修改内核或工程白名单；
- 本地开发和生产使用同一消费链，只在来源与 ActivationPolicy 上不同；
- Local Plugin Library 成为可由源码、远端和 Bundle 扩充的本地安装事实源；
- 代价是需要引入可恢复的撤销、来源追踪和跨内核调和契约，并逐步移除现有 Foundation 生成目录。

## 实施记录

- 2026-08-01（P1）：local-test Profile 可把 `candidate/stable` 作为只读/导入 channel，但普通发布仍由独立校验限制为 `workspace/testing`。新增 remote HTTPS Adapter/Resolver、Remote → Local Artifact Lock 同步器、Unix Socket 导入操作和 `platform-dev.sh plugin-library install`；安装先锁定远端 Catalog revision，再逐项下载完整依赖闭包，由共享契约校验锁的根、SemVer、闭包与依赖环，并由本地信任根复验证明、摘要、来源和安全状态。安全准入/复扫引用的原始报告先按摘要同步到本地私有归档，之后才允许制品入库。Catalog 以 `artifact.imported` Journal 事件持久保存上游 Receipt。相同远端来源幂等，跨来源、Catalog/Lock/Object 漂移、yanked/revoked 和直接本地 stable 发布均拒绝。导入不修改 Deployment 或 Portal Activation。
- 2026-08-01（P2）：默认热开发模式启动独立的源码输入适配器，扫描 `extensions/plugins` 与 `examples/plugins` 的直接子目录并持久保存源状态。新增或修改在防抖后构建内容寻址的 workspace 候选，只发布到 Local Plugin Library，不自动激活；更新遵循“发布新候选 → 持久记录待撤回引用 → 撤回旧候选”，重启可继续未完成撤回。目录删除产生单调 `artifact.withdrawn` Journal 事件；新 Catalog 快照与 Resolver 排除该候选，但活动 lease 的精确读取和引用保护继续有效，之后才进入普通 GC。同一内容的目录重新出现时会以受控生命周期事务恢复原不可变 ref，不需要伪造新版本。首次启用只建立现有源码基线并接管已经存在的 workspace 候选，不批量重建全部插件；stable/Seed 候选纳入统一 Inventory 由 P3 完成。
