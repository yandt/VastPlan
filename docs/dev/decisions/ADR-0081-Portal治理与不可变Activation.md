# ADR-0081 Portal 治理与不可变 Activation

- 状态：已采纳
- 日期：2026-07-19
- 修订：ADR-0059 第 6 项的进程生命周期固定 Profile、ADR-0075 第 7 项的 Catalog 不可热改，以及旧 Application 发布即成为活动 Portal revision 的语义
- 关联：[ADR-0059](ADR-0059-Frontend双输入服务端权威解析.md)、[ADR-0067](ADR-0067-Portal控制面闭环安全恢复与第二适配器验收.md)、[ADR-0073](ADR-0073-Portal内容寻址交付快照.md)、[ADR-0075](ADR-0075-Portal管理绑定与多平台基线.md)、[ADR-0076](ADR-0076-Portal-Edge分布式快照交付.md)

## 背景

Application Composition、Platform Profile 与 PortalBinding 都需要在线版本治理。若任一对象发布后直接改变线上 Portal，管理员无法区分“可被引用”和“已经上线”，回滚一个输入还可能撤销另一个输入的后续发布。当前 Composer 的进程固定 Catalog 和单文件状态也无法支持多实例故障接管、CAS、审计原子性和安全恢复。

## 决策

1. Application、Platform Profile、PortalBinding 是三个独立 revision 领域，均使用 `Draft → PendingApproval → Approved → Published`。Published 只表示可被引用，永不表示线上生效。
2. 新增不可变 `PortalActivation`，精确引用三份 Published revision 的 `id + revision + digest`、物化快照 digest、创建主体、时间和前一 Activation。只有 Activation 可以处于当前生效状态。
3. Profile 发布与 Portal 激活分离。Profile 可被多个 Portal 复用；管理中心分成 `Platform Profiles` 与 `Portals` 两个工作区，Portal 详情首先展示当前线上 Activation。
4. Activation 使用乐观两阶段：锁内读取候选和治理 revision；锁外验证、解析和物化；重新加锁并以 CAS 确认全部依赖与全局不变量仍然有效后提交 Activation 和审计。过期候选必须冲突失败。
5. 生产治理状态使用内核托管的 JetStream KV CAS 端口，按 tenant 保存一个治理聚合 revision，并绑定插件身份、tenant 与 leader fencing。开发环境提供安全本地文件适配器。
6. Profile、Binding、Activation 与审计在同一个治理聚合 CAS 中提交；外部内容寻址对象不能与 KV 原子提交，失败候选记录为 orphan，不立即删除。
7. 权限按领域分离：Application、Profile、Binding、Activation 分别拥有 read/write/approve/publish/activate/rollback 操作。浏览器动作必须同时通过 PortalBinding grant、Edge 路径角色和内核最终校验；Composer 不能自授目标或权限。
8. 激活流程为“选择三份已发布输入 → 当前信任与撤销校验 → 语义差异预览 → 物化 → Origin 提交 → Edge 后台预热 → CAS 激活”。中央 Origin 快照安全提交后可激活，单个离线 Edge 不阻塞；冷填充仍需校验不可变摘要。
9. 生产不实现 SSE 或轮询。新 Activation 在刷新、新标签页或重新打开时生效；API 响应携带 `X-VastPlan-Activation-Revision`，旧标签页只显示非阻塞刷新提示，禁止自动丢弃未保存状态。
10. 回滚选择历史 Activation，而不是分别回滚 Application/Profile/Binding。历史 tuple 必须按当前信任、撤销和兼容性重新校验，并创建新的 Activation revision。
11. 恢复路径由内核原生提供，独立于可能损坏的 Profile。若最新历史不安全，继续搜索更早的安全 Activation；没有安全候选时保持内核安全模式。
12. 本阶段不执行内容 GC。记录 orphan digest、时间和原因，提供容量告警；达到硬上限时拒绝新物化但不影响当前 Portal。完整可达性 GC 留作独立任务。

## 备选方案

- Profile 发布后直接重建所有引用 Portal：把共享基线发布等同于上线，变更半径不可控，拒绝。
- 分别维护三个“active revision”：无法形成原子线上事实，混合回滚会撤销无关发布，拒绝。
- Composer 继续使用内存加单 JSON 文件：无法在写失败、leader 接管和多实例并发下维持 revision/审计一致性，拒绝。
- 由浏览器选择 tuple 或目标服务：扩大 IDOR 和权限绕过面，拒绝。
- 允许 break-glass 恢复已撤销制品：突破当前信任边界，拒绝。

## 影响

- 正面：发布、上线、回滚和恢复拥有单一、可审计的线上事实。
- 正面：多 Composer、多 Edge 和 Portal 动态布局切换可在同一 Activation 语义下工作。
- 代价：现有 Portal Composer、BFF、状态存储和客户端 API 需要按领域重构。
- 代价：需要覆盖 CAS 冲突、崩溃点、leader fencing、跨租户授权、撤销恢复和多 Edge 预热的测试。

