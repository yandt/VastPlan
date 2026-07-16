# ADR-0028 控制器选主与 Drain 收敛

- 状态：已采纳
- 日期：2026-07-16
- 关联：[ADR-0024 单节点自动装配与回滚语义](ADR-0024-单节点自动装配与回滚语义.md)、[ADR-0025 NATS 控制面、能力寻址与多节点调度](ADR-0025-NATS控制面寻址与多节点调度.md)、[ADR-0027 NATS 生产安全与最小权限](ADR-0027-NATS生产安全与最小权限.md)

## 背景

ADR-0025 的 Controller 可以从全局 Deployment 生成节点 Assignment，但多个 Controller 副本会同时 watch 和写入。虽然 generation 使用 CAS，不会倒退，却仍会产生重复调度和不必要的 generation 竞争。插件宿主已有 DRAIN 指令，但宿主没有在入口阻止新调用，也没有区分“流已死亡”和“贡献/会话/进程已全部摘除”，导致 Close 返回后注册表仍可能短暂可见旧贡献。

## 决策

1. **每个 Deployment key 独立选主**。Controller 使用 `VASTPLAN_CONTROLLERS_V1` KV bucket 的 CAS 记录竞争领导权，不把所有租户/部署绑到一个全局 Leader。每个实例使用唯一 identity。
2. **领导权是短租约并带随机 fencing token**。默认租期 12 秒、约 4 秒续租；bucket TTL 为 15 秒。只有持有者运行 watcher 和 Scheduler。续租 CAS 失败后立即取消 leader context、停止写入并回到候选状态。
3. **释放和接管使用 revision fencing**。Leader 释放记录时携带最后 revision；过期持有者不能删除接任者的记录。接任者生成新的随机 token，日志必须记录 identity/election/token 供审计。
4. **调度仍维持可重试的最终一致语义**。Assignment 是多 key 写入，NATS KV 不提供跨 key 事务；中途失败由当前或接任 Leader 在 watcher/轮询中重算完整计划并以更高 generation 收敛，不能伪称原子提交。
5. **宿主入口 Drain 是原子的**。Drain 在同一互斥区标记不再接收新 Invoke/Event 并登记在途计数；随后等待在途调用归零，最后向插件发送 DRAIN。超时后上层可强制回收，但不能继续把新调用送进旧宿主。
6. **会话死亡与 teardown 完成分离**。`done` 只唤醒在途调用；`teardownDone` 在贡献摘除、会话表删除和进程回收全部完成后关闭。所有并发 Close/断流路径共享一次 teardown 并同步等待其完成。

## 备选方案

- **依赖 Scheduler generation CAS，不做选主**：能避免 generation 回退，但不能避免多个活跃写者和重复副作用，拒绝。
- **单个全局 Controller Leader**：实现更简单，但一个租户的故障会阻塞全部部署，拒绝。
- **引入 etcd/Consul**：能够提供成熟租约，但仅为选主新增一套控制面，与 NATS-first 方向冲突，拒绝。
- **Close 后由调用方轮询注册表**：把内部时序泄漏给所有调用方，拒绝。

## 影响

- 正面：Controller 可安全多副本部署并自动接管；旧宿主摘流、在途等待和贡献删除具有可测试的完成语义。
- 代价：选主依赖 NATS KV 时钟/TTL 和网络可用性；Assignment 跨 key 仍只保证最终一致，业务调用必须保持幂等与超时。
- 边界：严格跨 key 原子 Assignment、跨区域共识和有状态服务迁移仍需独立设计。
