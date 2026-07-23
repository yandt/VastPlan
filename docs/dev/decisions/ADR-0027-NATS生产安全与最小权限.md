# ADR-0027 NATS 生产安全与最小权限

- 状态：已采纳
- 日期：2026-07-16
- 关联：[ADR-0025 NATS 控制面、能力寻址与多节点调度](ADR-0025-NATS控制面寻址与多节点调度.md)、[ADR-0019 工程规范基线](ADR-0019-工程规范基线.md)、[系统架构 §2/§3](../architecture/系统架构.md)

## 背景

ADR-0025 跑通了 NATS KV、节点/capability 租约、request-reply 和多节点调度，但开发连接是明文匿名的。若直接用于生产，任意能访问端口的进程都可能读取全局部署、伪造节点租约、改写 assignment 或注册虚假 capability。仅加 TLS 只能保护链路，不能区分 bootstrap、controller、node 和 runtime 的职责。

## 决策

1. **生产连接同时要求 TLS 1.3、双向证书与 NKey**。CA 验证服务端和客户端证书，NKey nonce 签名把连接映射到 NATS 账号用户；缺少任一要素、seed 权限宽于 `0600` 或 URL 不是 `tls://` 均 fail-closed。
2. **静态账号分为 SYS 与 VASTPLAN**。SYS 只用于 NATS 系统事件；所有产品工作负载位于启用 JetStream 的 VASTPLAN 账号。账号内每个进程实例使用独立 NKey，不共享 node/controller seed。
3. **角色权限由代码生成，不手写复制**。`bootstrap` 可创建和校准 Stream/KV；`controller` 只读部署/节点并写 assignment；`node` 只读 desired/assignment/capability，写自己的 actual/node/capability，并参与 RPC/事件；`runtime` 只处理 capability/RPC/事件。
4. **JetStream API 按具体 KV Stream 放行**。非 bootstrap 角色不获得泛化 `$JS.API.>`；只允许打开和读取职责所需的 `KV_<bucket>` API。Node 对 Desired/Deployment/Assignment 的 `$KV` 发布显式 deny，防止全局意图被节点回写。
5. **明文匿名只保留显式开发模式**。现有 `Connect` 是测试兼容入口；产品命令统一使用 `ConnectWithConfig`，只有传入 `-nats-allow-insecure` 才允许本地 `nats://`，且该开关不能与部分安全参数混用。
6. **配置生成物默认不进入仓库**。`engineering/tools/natssecurity` 生成只含公钥和 TLS 路径的服务端配置，以及每实例独立的 `0600` seed。seed 和客户端私钥必须由秘密管理系统分发；仓库只保存运行指南和策略代码。

7. **NATS 消息还要绑定 addressing 工作负载身份**。生产 Node Agent 使用独立的传输 NKey 和信任文档，对 RPC、Core NATS 事件、JetStream 持久事件及能力目录租约签署 subject、payload、时间戳和 nonce；接收端验证签名后才重建 `CallContext` 的可信身份。持久事件重投可以跳过 nonce 重放判定，但不能跳过签名、subject、大小和身份校验。传输身份的 `NodeID` 必须与能力目录租约的 `node_id` 一致。
8. **Node ACL 必须绑定具体 NodeID**。节点只能发布自己的 `ACTUAL`、`NODE` 与能力租约 key；读取全局控制面只限职责所需的 metadata/KV API，不能通过通配符回写 Deployment、Desired 或 Assignment。
9. **宿主 SYSTEM 子身份必须双重授权**。默认仍按签名 transport identity 重建 SYSTEM caller；只有内核可信入口创建且在该身份 `allowedSystemCallers` 中精确列出的子身份可以跨传输保留。列表禁止通配符，Principal 必须清空，接收插件仍须按 caller/owner 做领域级授权。

## 后续补充（2026-07-23）

ADR-0116 为 Backend Platform Profile 在线激活增加 `catalog-publisher` 角色。该身份绑定一个精确 Catalog ID，只能读取和 CAS 写入其在 `VASTPLAN_BACKEND_PLATFORM_CATALOGS_V1` 中的确定性 key，不能写同 Bucket 其他 Catalog，也不能访问 Deployment、Desired、Assignment、Capability、配置授权或事件；Manager Node 仍为 Catalog 只读。可信 Backend 内核通过第二条 NATS 连接持有该能力，Deployment Manager 插件不取得 seed 或 KV 句柄。

ADR-0129 又为 Shared State 灾备增加相互分离的 `shared-state-backup` 与 `shared-state-restore`。两者都只能读取 `VASTPLAN_SHARED_STATE_V1` 并创建/删除该 stream 的短期只读扫描 consumer；backup 只获 snapshot API 和 flow ack，restore 只获空目标 restore API 和服务端分配的 restore subject。两者都没有 `$KV` 发布、stream 删除或其他控制面权限。

## 备选方案

- **用户名/密码 + TLS**：部署简单，但密码长期驻留且不具备 nonce 签名的私钥身份，拒绝作为生产默认。
- **只有 mTLS，不做 Subject ACL**：仍允许任一已获证书的节点改写控制面，拒绝。
- **每个角色独立 NATS Account**：隔离更强，但 RPC、事件和 KV 需要大量 import/export，当前会显著增加运维复杂度。先使用单应用账号内 NKey 用户 ACL；未来跨租户隔离可新增账号层。
- **所有角色允许 `$JS.API.>` 再靠应用代码约束**：无法抵抗被攻破的节点进程，拒绝。
- **只依赖 NATS TLS/NKey，不给消息签名**：已认证连接内的被攻破进程仍可伪造其他工作负载的 `CallContext` 或租约，拒绝。

## 影响

- 正面：链路窃听、匿名接入、节点改写全局意图、runtime 管理 Stream 等路径被真实 TLS/NKey/Subject 门禁阻断。
- 代价：证书和 NKey seed 需要按实例签发、轮换和撤销；新增 bucket 或调用面必须同步更新 ACL 并通过真实 NATS 权限测试。
- 边界：多租户独立账号、外部身份签发、OCSP/CRL 和跨集群 leaf/gateway 信任属于后续部署拓扑决策。
