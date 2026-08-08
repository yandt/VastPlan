# ADR-0200 Bootstrap 单元可信换版与双代 Open 屏障

- 状态：已接受
- 日期：2026-08-08
- 关联：[ADR-0147 种子与在线服务配置统一发布机制](ADR-0147-种子与在线服务配置统一发布机制.md)、[ADR-0169 Seed Recovery Capsule 与分阶段可用性](ADR-0169-Seed-Recovery-Capsule与分阶段可用性.md)、[ADR-0198 激活门禁活跃性与引导重试](ADR-0198-激活门禁活跃性与引导重试.md)、[平台控制数据库 Bootstrap](../architecture/平台控制数据库Bootstrap.md)

## 背景

Database Runtime 提供生产 Shared State，Deployment Manager 的耐久发布账本又建立在 Shared State 上。普通在线发布若要先由 Deployment Manager 写入新的 Database Runtime desired revision，就会要求即将被替换的 Database Runtime 先持续提供这次发布所依赖的状态服务。

`startup_tier=bootstrap` 只绕过 Full 单元激活前的 Shared State 门禁。冷启动可以从已持久化的 NATS Deployment KV 恢复，但热换版仍存在发布者依赖被换版 Provider 的活性环。Node Agent 原来的提交顺序还会在候选能力完成路由注册后立即停止旧代，Platform Control 的 topology reconcile 尚未来得及对候选实例执行 `Open`，从而留下短暂或永久的 Shared State 空窗。

## 决策

同时采用两个互补边界。

### 1. 可信 Bootstrap 发布通道

NATS Deployment v2 继续是唯一 desired truth，发布器在组合根固定三种不可由下游覆盖的 lane：

- `application`：Deployment Manager 在线发布使用；允许普通 Full 单元变更，但拒绝创建、删除或修改任何 `startup_tier=bootstrap` 单元。
- `bootstrap`：只由可信宿主的 `controlplane -bootstrap-unit-release` 使用；要求 Deployment 已存在，只允许现有 Bootstrap 单元中既有插件身份的精确制品引用和对应 baseline/Profile 引用随本次解析结果变化。配置、拓扑、副本、资源、插件身份、Full 单元和应用组合均不得同时变化。
- `seed`：只用于显式 `controlplane -bootstrap` 的首次创建与恢复根操作。

lane 校验在读取当前 Deployment 后、使用同一个 KV revision 执行 Create/Update CAS 前完成。校验和 CAS 不得分别读取 current，否则并发普通发布可能被受限换版意外覆盖。

该通道不新增插件或普通网络 API。它是 Backend Kernel 中的可信宿主工作流，权限边界仍由 controlplane 进程身份、NATS 凭据和主机文件权限共同承担。

### 2. 双代 Open 屏障

Node Agent 把候选替换拆成以下顺序：

1. 启动、校验并登记候选 generation；
2. 激活候选路由，但保持旧 generation 活动；
3. 对热替换的 Bootstrap 单元，把候选 Runtime Instance ID 交给协议中立的 replacement barrier；
4. Platform Control 只选择同时贡献 `foundation.state.shared.sql.bootstrap` 的候选实例，等待它们进入 `openReplicas` 最近一次全部成功后的副本集合；
5. 屏障成功后提交候选为当前 generation，再关闭旧路由并 drain 旧进程。

首次冷启动、Full 单元和不贡献 Platform Control Bootstrap capability 的其他 Bootstrap 单元不等待该屏障。等待上限为 30 秒；超时、Open 失败或并发 generation 已变化时，候选回滚且旧代保持活动。

## 理由

只采用可信发布通道能断开 Deployment Manager 对 Shared State 的写入依赖，但不能消除 Node Agent 在候选尚未 Open 时停止旧代的服务空窗。只采用双代重叠能改善运行期接管，却仍要求 Deployment Manager 借助被换版的 Provider 完成 durable publication。A 与 B 分别处理发布活性和运行期接管，缺一不可。

复用 `openReplicas` 的 all-success Replace，使屏障等待的不是进程存活或能力已登记，而是精确候选实例已用当前 Platform Control Profile 成功打开数据库。Node Agent 只认识通用候选证据和屏障接口，不认识 Database Runtime 插件 ID 或 Platform Control 协议。

## 影响

- 普通 Deployment Manager 发布若包含 Bootstrap 单元变化会在 NATS desired CAS 前失败；它不能再承担这类换版。
- Bootstrap 插件换版和回滚必须由可信宿主单独发布，并使用新的单调 Deployment revision；不能与业务配置或 Full 单元变更合并。
- 候选 Open 期间新旧两代同时占用 Runtime 和数据库连接预算，容量规划必须继续覆盖 active-active 双代上界。
- 屏障失败不会回滚已发布 desired revision；Node Agent 保留旧代并继续重试同一 desired，形成可观测、可恢复的 pending 状态。
