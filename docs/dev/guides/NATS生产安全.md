# NATS 生产安全运行指南

本指南落实 ADR-0027。生产环境不得使用 README 中带 `-nats-allow-insecure` 的本地开发命令。

## 1. 准备企业 CA 与实例证书

为 NATS Server 签发 ServerAuth 证书；为每个 controller、Node Agent 和独立 runtime 签发各自的 ClientAuth 证书。证书私钥由秘密管理系统分发，不提交到仓库。

## 2. 生成 NKey 与服务端 ACL

```bash
go run ./engineering/tools/natssecurity \
  -out /secure/vastplan-nats \
  -listen 0.0.0.0:4222 \
  -store-dir /var/lib/vastplan/nats \
  -tls-cert /etc/vastplan/nats/server.crt \
  -tls-key /etc/vastplan/nats/server.key \
  -tls-ca /etc/vastplan/pki/ca.crt \
  -tenant acme \
  -deployment cluster \
  -controller-count 3 \
  -catalog-publisher-count 1 \
  -catalog-id backend-production \
  -node-count 10 \
  -manager-node-count 1 \
  -runtime-count 3
```

工具不会覆盖已有文件。`*.seed` 均为 `0600` 且每个实例独立；`nats-server.conf` 只含公钥、ACL 和证书路径。`node` 与 `manager-node` 的 ACL 固定绑定到生成时声明的 tenant、Deployment 与 cluster-global node ID；不同作用域必须分别生成身份，重复 node ID 会被拒绝。

除 NATS 连接用的角色 seed 外，跨节点 addressing 还需要为每个 Node Agent 单独生成一枚传输签名 NKey，并把所有允许互调的公钥登记到 `transport-trust.json`（文件权限至少 `0600`）。传输 seed 不得复用 bootstrap/controller/node/manager-node/catalog-publisher 的 NATS seed；信任文档随发布配置原子更新并纳入密钥轮换流程。

## 3. 启动 NATS

```bash
nats-server -c /secure/vastplan-nats/nats-server.conf
```

生产 JetStream 至少三副本。NATS 集群节点间 Route TLS 需随实际拓扑另行配置，但客户端 mTLS/NKey/ACL 不变。

`VASTPLAN_SHARED_STATE_V1` 保存插件跨实例状态，必须纳入 JetStream 三副本、磁盘容量、快照备份和恢复演练。Node 身份可读写该 bucket 是因为 Shared State Provider 位于可信 Backend 进程；插件进程不取得 Node NKey。Provider 故障时不得回退本地 File Store。value 可能包含业务私有状态，运维日志和支持包不得导出明文内容。

## 4. 初始化 Bucket

```bash
go run ./core/kernels/backend controlplane \
  -nats-url tls://nats.example.com:4222 \
  -nats-ca /etc/vastplan/pki/ca.crt \
  -nats-cert /etc/vastplan/pki/bootstrap.crt \
  -nats-key /etc/vastplan/pki/bootstrap.key \
  -nats-seed /secure/vastplan-nats/bootstrap.seed \
  -bootstrap -replicas 3 \
  -platform-profile /etc/vastplan/platform-profile.json \
  -application-composition /etc/vastplan/application-composition.json \
  -deployment-revision 1 \
  -repository /var/lib/vastplan/repository
```

bootstrap seed 只在初始化/迁移作业中挂载，常驻 controller 和 node 不得持有。
服务配置入口不接受人工编写的 Deployment v2。上述两份输入会先经 Composition Resolver
校验插件分级与来源，生成锁定输入摘要的 Deployment v2，再由 Controller 消费。

## 5. 运行 Controller 与 Node Agent

Controller 使用独立 `controller-N.seed`：

```bash
go run ./core/kernels/backend controlplane -controller \
  -nats-url tls://nats.example.com:4222 \
  -nats-ca /etc/vastplan/pki/ca.crt \
  -nats-cert /etc/vastplan/pki/controller-1.crt \
  -nats-key /etc/vastplan/pki/controller-1.key \
  -nats-seed /secure/controller-1.seed \
  -repository /var/lib/vastplan/repository \
  -key tenants.X2dsb2JhbA.states.Y2x1c3Rlcg
```

Controller 必须能读取与 Node Agent 同源的不可变制品仓库，用于在生成 Assignment 前解析制品中的完整 manifest，包括 runtime capability、包依赖和版本范围。仓库不可读、制品缺失或清单身份不一致时调度 fail-closed，不发布半份计划；制品签名仍由 Node Agent 安装链路验证。

普通 Node Agent 使用属于本节点的 `node-N.seed`：

```bash
bin/backend-kernel reconcile \
  -nats-url tls://nats.example.com:4222 \
  -nats-ca /etc/vastplan/pki/ca.crt \
  -nats-cert /etc/vastplan/pki/node-1.crt \
  -nats-key /etc/vastplan/pki/node-1.key \
  -nats-seed /secure/node-1.seed \
  -transport-seed /secure/node-1.transport.seed \
  -transport-trust /secure/transport-trust.json \
  -tenant acme -deployment cluster -node-id node-1
```

承载 Deployment Manager 的节点改用独立的 `manager-node-N.seed`。该角色只增加 Nodes 与 Backend Platform Catalog 读取能力，用于可信内核观察 readiness 和解析活动 Profile；不增加 Deployment、Desired、Assignment 或 Catalog 写权。启用 `-backend-platform-catalog` 时还必须向该可信内核单独挂载 `catalog-publisher-N.seed`，并通过 `-catalog-publisher-nats-seed` 指定。内核用第二条 NATS 连接执行候选 Catalog CAS，Deployment Manager 插件本身拿不到 seed、KV 或 Catalog 全文。引导计划中的 `transportPublicKey` 必须是目标节点 transport seed 对应的公钥。

未显式配置 `-transport-seed` 与 `-transport-trust` 时，控制面 Node Agent 会拒绝启动；仅本地开发可以显式使用 `-nats-allow-insecure` 绕过 NATS 与 addressing 的生产门禁。传输信封会绑定 subject、payload、时间戳和 nonce，接收端据此校验签名并重建可信调用身份。默认情况下，线上的 `SYSTEM` caller 始终重建为签名 transport identity，不能自报；只有可信宿主创建、且在该 transport identity 的 `allowedSystemCallers` 中逐项精确列出的 SYSTEM 子身份可以保留，通配符、用户 Principal 和插件自报均无效。JetStream 重投只跳过 nonce 重放检查，不跳过签名和身份校验。

`transport-trust.json` 中每个身份还必须显式声明调用边界：

```json
{
  "version": 1,
  "identities": [{
    "name": "database-node-1",
    "role": "node",
    "publicKey": "U...",
    "nodeId": "node-1",
    "tenantId": "acme",
    "serviceRoles": ["backend"],
    "logicalServices": ["platform.database", "platform.credentials"],
    "allowedCapabilities": ["platform.settings", "platform.credentials", "platform.credentials.material-lease"],
    "allowGlobal": false
  }]
}
```

`tenantId` 必须与该节点启动参数和首次引导计划一致。`allowedCapabilities` 是第一道 capability allowlist；`service` 还要求 service role 匹配，`cluster` 还要求 logical service 匹配，`global` 必须单独设置 `allowGlobal=true`。需要使用托管凭证的可信宿主必须同时声明 `platform.credentials.material-lease` 和目标 `platform.credentials` logical service；不执行敏感操作的节点不要授予。`*` 只允许在受控迁移期使用，不应作为生产默认值。一元 NATS 调用和 gRPC 双向流使用同一授权规则。

Node Agent 每次续租都会对完整 v3 Lease 做 detached signature。Lease key 同时编码 tenant、Deployment 与 node ID；Controller 只调度作用域完全匹配的 v3 Lease，Deployment Manager 只通过 `kernel.node.readiness` 获取封闭观察结果。普通插件不得直接读取 Nodes KV 或 transport trust。

## 6. 权限边界

| 角色 | 允许 | 明确禁止 |
|---|---|---|
| bootstrap | 创建/校准全部 KV 与发布初始配置 | 常驻业务进程持有 |
| catalog-publisher | 只读写身份绑定的精确 Backend Platform Catalog key | 写同 Bucket 其他 Catalog，或访问 Deployment/Desired/Assignment/Capability/配置授权/事件 |
| controller | 读 Deployment/Node/Actual/Autoscaling Metric，写 Assignment/Composition/Controller Lease | 改 Stream、写 Actual/Capability/Metric |
| node | 读 Desired/Assignment，写自身作用域的 Actual/Node、Capability/Autoscaling Metric，RPC/事件 | 读取其他节点 Lease，写 Deployment/Desired/Assignment |
| manager-node | node 的全部权限，额外读取 Node Lease 与活动 Backend Platform Catalog | 写 Deployment/Desired/Assignment/Catalog 或其他节点 Lease |
| runtime | Capability、RPC、事件、写 Autoscaling Metric | 读取或改写部署控制面 |

新增 bucket 或 Subject 时，必须先修改 `RoleACL` 并增加真实权限允许/拒绝测试，不得在部署配置里临时放开 `>`。
