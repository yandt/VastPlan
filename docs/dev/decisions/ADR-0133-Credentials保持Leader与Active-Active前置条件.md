# ADR-0133：Credentials 保持 Leader 与 Active-Active 前置条件

- 状态：已采纳；A5 就绪评估完成，保持 Active-Passive
- 日期：2026-07-23

## 背景

Credentials 已将租户安全账本迁入 Shared State，具备 Root CAS、跨节点接管、Material Lease 二次复核、签名备份恢复、容量上限和孤儿 chunk 安全 GC。下一步需要判断是否应像 global-settings、plugin-settings 和 Portal Composer 一样切换为 Active-Active。

Credentials 是低频安全控制面，但包含普通 CAS 之外的不可逆或跨系统动作：Vault Transit encrypt/rewrap、一次性 ConfigurationAuthority consume、Material Lease decrypt，以及只有唯一维护 owner 才能执行的 chunk mark/sweep。仅证明“两个实例只有一个 Root CAS 成功”不足以证明整个业务操作可安全重放。

## 评估矩阵

| 路径 | 双实例 CAS 结果 | 外部/一次性动作 | 当前结论 |
|---|---|---|---|
| describe/list/audit | 只读 | 无 | 可多副本读取，但当前 capability 与 GC 前置维护仍由 Leader 提供 |
| put/stageManaged | 单 Root 胜出 | Vault encrypt 可重复，随机 handle 会变化 | 需要 durable operation ID 才能消除超时与重试歧义 |
| rotate | 单 Root 胜出 | Vault rewrap 可能重复 | 不破坏密文，但调用结果无法仅靠 CAS 判定是否已提交 |
| revoke/状态转换 | 单 Root 胜出 | 无 | 目标状态幂等，审计与状态保持同 Root 原子提交 |
| stageDelegated | 单 Root 胜出 | **先消费一次性 Authority，再 encrypt/提交 Root** | CAS 冲突会令授权已消费但凭证未落账，当前硬阻断 |
| Material Lease | Root 前后复核 | Vault decrypt | 已能让并发 revoke/retire 胜出；不需要 Active-Active 写入 |
| orphan chunk GC | marker/delete CAS | 需要当前 writer owner | A4 明确使用 Unit Leadership fenced mutation，Active-Active Unit 无唯一 owner |

自动化已覆盖 stale Root CAS、双实例读取、decrypt/retire 竞态、fenced mutation 缺失/错 Unit 拒绝，以及 `stageDelegated` 在 consume 后 CAS 冲突时无法用同一一次性 Authority 重试。

## 方案比较

### 方案 A：继续 Leader + External-Shared + Leader Routing（采用）

所有写入、租户 lazy maintenance、GC 和 Material Lease 由当前 Unit Leader 承载；故障后新实例使用更高 epoch 接管。优点是 owner、一次性 Authority、Vault 调用和 Root 提交顺序单一，现有负载也不需要写扩展。代价是单实例吞吐，但 Credentials 不属于高频数据面。

### 方案 B：业务 Active-Active + 独立 Maintenance Leader（未来候选）

普通业务实例使用 CAS，GC/维护由单独的 tenant-scoped owner 执行。每个 mutating 请求先以宿主签发的 durable command identity 建立命令记录，Authority consume、Vault 操作与 Root 提交通过 `prepared -> side-effected -> committed` outbox 恢复。该方案扩展性最好，但需要新的命令真源、结果缓存、租户调度和清理策略；不能为切换清单临时拼装。

### 方案 C：全部 Active-Active，仅依赖 Root CAS（拒绝）

它能保护最终 Root，却不能恢复已消费 Authority、区分超时提交结果，也不能为 GC 提供唯一删除 owner。会把安全错误变成偶发业务失败，拒绝。

## 决策

1. Credentials 0.12.x 保持 `leader + external-shared + cluster + leader routing`，并用架构测试锁定该清单，不把 Active-Active 当作默认成熟度指标。
2. Root、chunk 和 GC mutation 继续只使用 fenced Shared State；禁止为了多副本写入退回普通 create/update/delete。
3. 当前 active-passive 已具备生产所需的跨节点接管和 stale writer fencing。读取吞吐若未来成为瓶颈，优先评估只读缓存/副本，而不是放宽写 owner。
4. 重新评估 Active-Active 必须同时完成：
   - 宿主签发、跨重试稳定且不可伪造的 durable command identity；
   - Authority consume 与 Root 提交的可恢复 outbox，不持久化用户 CallContext；
   - Vault encrypt/rewrap 的 operation result 对账和超时不确定结果处理；
   - 独立、可信、tenant-scoped maintenance owner，继续使用 fenced delete；
   - 命令结果保留/回收、重复调用和跨实例故障矩阵；
   - 明确收益证据，证明单 Leader 已成为真实容量瓶颈。
5. 实现语言继续使用 Go，运行在现有可信独立进程。未来命令层也应先复用 Go 的 Shared State CAS、operation fence、内存清零和 Vault 适配；若某个 Provider 生态要求其他语言，只通过窄协议连接，不迁移 Credentials schema owner。

## 影响

正面：不为形式上的多副本牺牲一次性授权和删除安全；当前模型边界清晰、可接管、可恢复，且符合低频凭证控制面负载。Active-Active 的前置条件成为可验证清单，不再靠“CAS 应该够用”的推断。

代价：一个租户的 Credentials 操作仍由单 Leader 串行；Leader 故障期间需要等待 lease 接管。企业环境仍需记录实际接管时间和 Vault HA 恢复时间。

## 验收

- Manifest 架构测试固定 leader policy、fenced mutation 和普通 mutation 禁止项；
- 一次性 Authority 在 CAS 冲突后不可重用的测试持续证明当前 Active-Active 阻断；
- stale Root writer、Material Lease 与 retire 竞态、三节点接管和 Vault 故障矩阵保持通过；
- 未来若修改为 Active-Active，必须先修改本 ADR 状态或新增替代 ADR，并为上述全部前置条件提供代码与故障测试证据。

