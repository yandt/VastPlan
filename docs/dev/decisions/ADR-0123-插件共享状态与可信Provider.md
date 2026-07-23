# ADR-0123 插件共享状态与可信 Provider

- 状态：已采纳，基础能力与首个消费者已实现
- 日期：2026-07-23

## 背景

大量平台插件仍以本地 JSON 保存状态。leader 放置只能避免同时写，不能在节点故障后让新实例取得同一份数据；直接把每个插件改成 NATS/SQL 客户端，又会复制凭证、ACL、CAS、租户隔离、限制和错误处理，并使 Python/Node 插件分别持有基础设施权限。

## 决策

1. Shared State 是 Backend 内核的通用运行能力，不是业务插件。内核只承载身份派生、协议校验和 Store SPI；业务 schema、迁移和工作流仍由插件拥有。
2. 插件通过五个独立、可在签名清单逐项授权的 `kernel.state.shared.*` capability 调用；不增加可被任意插件贡献的公共扩展点。
3. tenant、plugin ID 和 RuntimeScope 由可信宿主生成。请求只能选择 `tenant|service` scope kind 与局部 namespace/key，不能自报身份或物理 key。
4. v1 只承诺单 key Create/Update/Delete CAS 和有界弱一致 List，不提供虚假跨 key 事务。错误码固定为 `state.not_found`、`state.conflict`、`state.invalid`、`state.unavailable`。
5. NATS KV 是首个集群 Provider；File 是单进程开发 Provider，禁止生产自动回退。SQL Provider 延后但必须实现同一 Store SPI。
6. 核心 Provider 使用 Go：现有控制面、NATS JetStream、RuntimeIdentity 和 host service 都在 Go，Go 对常驻连接、CAS 与资源边界最合适。Node/Python 只实现各自 Runtime 的薄客户端；其他语言按 JSON Schema增加客户端。运行方式不新增进程。
7. 首个迁移消费者是 global-settings；凭证服务因涉及持久密文和审计，待普通状态路径验证后最后迁移。

## 备选方案

- **每插件直接连接 NATS/SQL**：基础设施凭证和隔离逻辑扩散到插件，拒绝。
- **独立远端 State Service 插件**：可替换性高，但形成所有插件启动依赖、额外网络跳转和自举状态；当前 Store SPI 已能替换 Provider，不采用。
- **继续 leader + 共享文件卷**：依赖部署文件锁与卷一致性，无法提供稳定 CAS/fencing，不作为通用集群方案。
- **一开始提供跨 key 事务**：NATS KV 无此语义，模拟会隐藏崩溃窗口；使用单文档提交点与显式 Saga。

## 影响

正面：多语言插件不持有存储凭证；身份隔离和 CAS 只有一套真源；同一插件副本可安全共享状态；Provider 可在不改变业务协议的前提下替换。

代价：大状态需重新建模，不能滥用 1 MiB value；List 不是事务快照；现有本地文件插件需逐个设计迁移；NATS bucket 进入生产备份、容量和 ACL 范围。

## 实施记录

- Shared State 已完成严格 Wire、可信宿主身份派生、NATS KV/File Provider、NKey ACL 和 Go/Node/Python SDK。
- global-settings 0.8.0 已将每 tenant 的完整状态聚合为单 key CAS 文档，切换为 `active-active + external-shared + queue`；清单只申请 get/create/update 三项最小内核能力。
- plugin-settings 0.12.0 已按 [ADR-0124](ADR-0124-Plugin-Settings租户聚合与Active-Active协调.md) 将每 tenant 的候选、审批、凭证阶段、激活 Saga 与审计聚合为单 key CAS 文档，切换为 active-active；Scoped watch 以本地即时通知加跨实例一秒观察实现。
- Portal Composer 1.6.0 已按 [ADR-0125](ADR-0125-Portal-Composer与Preference共享状态分区.md) 将组合治理和用户偏好迁入 Shared State，并切换为 active-active。
- Deployment Manager 0.17.0 已按 [ADR-0126](ADR-0126-Deployment-Manager共享账本与副作用Fencing.md) 迁移租户账本；因 SSH/systemd/发布副作用尚未携带 fencing token，保持 `leader + external-shared + leader routing`。
- 单元测试覆盖两个插件实例共享读取、tenant 隔离、身份字段不进入请求、并发更新单赢家，以及 NATS 中断时 fail-closed、原存储目录重启后的自动重连恢复。真实跨进程 E2E 已验证实例 A 写入并退出后实例 B 从同一 NATS KV 继续读取。旧 `platform.settings.stateFile` 已从清单与开发 Profile 删除。
