# ADR-0172 通用版本账本与可插拔存储 Provider

- 状态：已采纳，P1 Ledger 与 File Provider 已实施
- 日期：2026-07-30
- 关联：[ADR-0081](ADR-0081-Portal治理与不可变Activation.md)、[ADR-0123](ADR-0123-插件共享状态与可信Provider.md)、[ADR-0150](ADR-0150-插件共享代码与能力复用边界.md)、[ADR-0152](ADR-0152-插件边界门禁与权限策略物理收敛.md)、[ADR-0171](ADR-0171-Portal单聚合版本与上线子流程.md)

## 背景

Portal Composer、Deployment Manager、Plugin Settings 等领域都保存不可变配置版本，但它们对版本内容、历史链和存储介质各自实现。继续复制文件、Shared State 或数据库代码会让摘要、幂等、损坏检测和历史查询逐渐漂移；反过来，把审批、上线、回滚和权限一并抽入“万能版本插件”，又会破坏领域聚合边界。

同时需要支持三种不同环境：开发/种子环境使用纯文件，审阅或 GitOps 场景使用 Git，生产集群使用关系数据库。存储选择不能出现在浏览器请求或业务插件代码中，也不能让每一种 Provider 必然形成一个独立进程。

## 决策

1. 新建基础能力 `cn.vastplan.foundation.versioning.ledger`。它满足独立插件资格：拥有状态、在线配置、独立版本、可替换 Provider 和集群生命周期；它不是内核能力。
2. `version.ledger.v1` 只负责：不可变 JSON 版本、父版本链、内容摘要、幂等写入、精确读取、历史遍历，以及可选的命名 Head CAS。审批、发布、上线、回滚、业务权限和版本可见性仍由调用方领域插件负责。
3. Ledger 插件采用 Database Runtime 式 Provider 模型：第一方 Provider 可作为同一 Go 插件进程内的模块注册；未来第三方或异构 Provider 通过同一版本化 RPC SPI 接入。Provider 类型不等于物理进程。
4. v1 预留并规范三种协议：
   - `version.storage.file.v1`：本机/受管卷，默认只承诺单 writer；
   - `version.storage.git.v1`：以 commit/ref 表达不可变版本和 CAS Head，主要用于审阅、导入导出与 GitOps；
   - `version.storage.relational.v1`：使用事务和唯一约束提供线性一致 CAS，是生产集群首选，且通过 Database Runtime 支持多种数据库，不绑定 PostgreSQL。
5. namespace 到 Provider 实例的路由属于 Ledger 的受信插件配置。调用方只提交 namespace、stream 和版本内容，不能选择 Provider、连接、仓库路径或凭证。
6. 版本写入分为 create-only `putVersion` 与独立 `moveHead`。Ledger 校验并规范候选内容、投影可信 actor，并按 `version.identity.v1 = SHA-256(domain + tenant + namespace + streamId + idempotencyKey)` 统一派生逻辑 versionId；Provider 使用可信 tenant 复核 ID，并在持久化原子边界内落实唯一约束及分配 sequence、createdAt，避免不同存储协议产生不同身份或多实例在事务外争抢序号。领域插件可以先幂等写入不可变版本，再提交自己的聚合 CAS，最后通过 outbox 镜像 Head；因此不伪造跨插件事务。失败写入最多留下不可达版本，不会提前改变业务当前值。
7. Portal Composer 后续只把完整 `PortalConfiguration` 内容迁入 `portal.configuration` namespace；其 Root CAS 继续保存 PortalVersion 状态、VersionRef、Release、Test Release 和审计。正式 Portal Head 是镜像/检索加速，不取代 Portal 聚合真相源；测试候选使用隔离 namespace，不进入正式 Head。
8. Ledger v1 不保存密文或明文秘密。版本内容只能是有界规范 JSON；密码、token、私钥等必须留在 Credentials 插件，版本内容只保存 `CredentialRef`。摘要只由可信 Ledger 按协议规范化结果计算，异构 Provider 必须使用官方 SDK 与 golden vectors，不能依赖各语言默认 JSON 序列化。大于 1 MiB 的二进制或包体继续进入制品/对象仓库。
9. 每个 Provider 必须通过同一契约套件：create-only 幂等、同 key 不同内容冲突、内容摘要复核、父链闭合、Head CAS 单赢家、跨 stream/tenant 拒绝、损坏 fail-closed、分页稳定和重启恢复。

## 语言与运行方式

- Ledger 协调器、File Provider 和首个 Relational Provider 选择 Go：当前 Portal/Shared State/Database Runtime 契约均在 Go，资源占用、并发和原子文件能力最合适。
- Git Provider 实施前重新比较 Go `go-git`、Rust `git2`、Python Dulwich 和 Node Git 生态；默认优先 Go，但不为了语言统一牺牲大仓库性能或协议完整度。
- 第一方 Provider 默认进入 Ledger 所在 Go 共享 Runtime 进程。只有第三方隔离、原生库冲突、独立扩缩容或企业策略要求时才运行独立进程。

## 否决方案

- **Portal 内部实现 File/Git/DB 三套存储**：交付快但无法复用，摘要和 CAS 规则会继续复制，否决。
- **版本插件接管所有领域状态机**：会把审批、上线和权限变成条件分支大包，否决。
- **全平台立即事件溯源**：迁移面和运行复杂度远超当前收益，否决。
- **Git 作为所有生产写入的默认数据库**：远端 ref CAS、锁、查询和高并发延迟不适合作为通用在线主存储，否决。

## 实施顺序

1. P0：固化 JSON Schema、Go 强类型、请求/响应解析、稳定错误码和 Provider RPC SPI。
2. P1（已完成）：建立 Ledger 插件骨架、内存契约 Provider 和 File Provider，完成崩溃恢复与一致性测试。
3. P2：Portal Composer 通过 VersionRef 保存配置；先写不可变版本，再 Root CAS，再 outbox 镜像 Head。
4. P3：通过 Database Runtime 实施 Relational Provider，完成 active-active 与故障矩阵。
5. P4：按真实 GitOps 需求选型并实施 Git Provider，不让 Git 依赖进入 File/Relational 运行闭包。

## 影响

正面：版本语义、摘要和 Provider 切换形成单一协议；Portal 保持领域内聚；开发文件、企业数据库和 GitOps 可以按 namespace 选择；Provider 可共享进程而不制造进程风暴。

代价：Portal 与 Ledger 之间需要可恢复 outbox；版本内容读取增加一次能力调用；未实施安全 GC 前，不可达版本只能保守保留。Ledger 故障会阻止新的治理写，但不得影响已发布 Portal Runtime 使用现有 Activation。
