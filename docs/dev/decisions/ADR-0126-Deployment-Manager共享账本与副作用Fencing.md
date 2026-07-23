# ADR-0126 Deployment Manager 共享账本与副作用 Fencing

- 状态：已采纳，共享账本与统一 Leader fencing 已实现；active-active 不在当前边界
- 日期：2026-07-23

## 背景

Deployment Manager 原先把全部租户状态保存到单机 JSON 文件。leader 放置避免了正常情况下的并发写，却使备用实例无法在节点故障后取得节点计划、Bootstrap Job、服务组合 revision、配置/Profile 激活 Saga 和测试发布状态。直接切换 active-active 又不安全：SSH、systemd、Deployment 发布和 Catalog 激活属于外部副作用，Store CAS 无法撤回已发出的操作。

## 决策

1. 0.17.0 将每个租户的完整控制账本聚合到 `tenant/deployment.control/tenant`，通过 `kernel.state.shared.get/create/update` 与 Store revision CAS 持久化。插件不直接取得 NATS、SQL 或文件系统凭证。
2. 运行模型先改为 `leader + external-shared + cluster + leader routing`。备用实例接管后读取同一账本；旧实例持有的 revision 不能覆盖新提交。
3. 新执行实例首次装载某租户时执行一次恢复：未确认的 SSH 引导 fail-closed 为 `platform.deployment.interrupted`；非终态 Test Release 标记失败并要求显式回滚；活动制品引用重新进入幂等 outbox 校验。恢复结果也必须通过 CAS 提交。
4. Shared State CAS 只 fence 状态写入，不把插件宣称为 active-active。外部副作用按 [ADR-0128](ADR-0128-统一Leader-Epoch与外部副作用Fencing.md) 复用 Unit Leadership 的 epoch/token：可信宿主动态校验 current evidence，Deployment/Catalog 保持 revision CAS，SSH 远端记录单调 epoch 并拒绝旧 owner。状态机仍需在超时后先查询真实结果再决定继续、回滚或人工处置。
5. 进入 active-active 的验收门槛包括：双实例争抢单赢家、旧 owner 延迟回包拒绝、租约过期接管、SSH 已执行但回包丢失、Catalog 已切换但账本未提交、Deployment 发布幂等重放以及真实多节点故障演练。未通过时只允许单 Leader 执行写工作流。
6. 当前处于开发阶段，不迁移旧本地文件；删除 `VASTPLAN_DEPLOYMENT_MANAGER_STATE_FILE`。生产环境若已有历史数据，必须另做带摘要、冻结窗口和回滚点的一次性迁移事务。
7. 插件继续使用 Go：现有状态机、Backend Kernel 窄服务和安全契约均在 Go，常驻资源与并发控制更合适；Node/Python 在本领域没有足以抵消跨语言重写与副作用一致性风险的生态优势。运行方式仍是现有可信 Go 插件进程，不新增进程。

## 备选方案

- **直接 active-active**：账本可以 CAS，但外部操作没有 fencing，可能由旧实例在失去所有权后继续执行，拒绝。
- **继续本地文件 + leader**：实现简单但无法跨节点恢复，拒绝。
- **让插件直接持有 NATS/数据库连接**：扩大凭证与租户隔离面，拒绝。
- **每个工作流单独建 key**：可降低冲突，但 v1 没有跨 key 事务，多个索引与审计容易部分提交；当前先保留 tenant 单提交点，容量接近 1 MiB 时再按有明确不变量的聚合拆分。

## 影响

正面：备用实例能读取完整控制账本；stale writer 不能覆盖新 leader；本地状态路径和文件权限配置从部署面删除；租户隔离由宿主身份派生。

代价：单 Leader 内的工作流仍串行；大租户可能接近单值上限；外部副作用 active-active 仍需独立工程阶段。共享账本可提高故障恢复能力，但不等同于外部操作 exactly-once。

## 验证

- 两个 Service 实例共享读取同一 tenant，另一个 tenant 不可见；
- 两个实例基于同 revision 更新时只有一个 CAS 成功；
- 新实例只执行一次中断引导恢复；
- 清单与平台 Profile 锁定 `external-shared + leader`，访问策略仅授予精确 Shared State 操作；
- 全量 Go、race、Portal 生命周期和发布门禁共同验证。
