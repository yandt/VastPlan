# NATS 生产安全运行指南

本指南落实 ADR-0027。生产环境不得使用 README 中带 `-nats-allow-insecure` 的本地开发命令。

## 1. 准备企业 CA 与实例证书

为 NATS Server 签发 ServerAuth 证书；为每个 controller、Node Agent 和独立 runtime 签发各自的 ClientAuth 证书。证书私钥由秘密管理系统分发，不提交到仓库。

## 2. 生成 NKey 与服务端 ACL

```bash
go run ./tools/natssecurity \
  -out /secure/vastplan-nats \
  -listen 0.0.0.0:4222 \
  -store-dir /var/lib/vastplan/nats \
  -tls-cert /etc/vastplan/nats/server.crt \
  -tls-key /etc/vastplan/nats/server.key \
  -tls-ca /etc/vastplan/pki/ca.crt \
  -controller-count 3 \
  -node-count 10 \
  -runtime-count 3
```

工具不会覆盖已有文件。`*.seed` 均为 `0600` 且每个实例独立；`nats-server.conf` 只含公钥、ACL 和证书路径。

除 NATS 连接用的角色 seed 外，跨节点 addressing 还需要为每个 Node Agent 单独生成一枚传输签名 NKey，并把所有允许互调的公钥登记到 `transport-trust.json`（文件权限至少 `0600`）。传输 seed 不得复用 bootstrap/controller/node 的 NATS seed；信任文档随发布配置原子更新并纳入密钥轮换流程。

## 3. 启动 NATS

```bash
nats-server -c /secure/vastplan-nats/nats-server.conf
```

生产 JetStream 至少三副本。NATS 集群节点间 Route TLS 需随实际拓扑另行配置，但客户端 mTLS/NKey/ACL 不变。

## 4. 初始化 Bucket

```bash
go run ./kernels/backend controlplane \
  -nats-url tls://nats.example.com:4222 \
  -nats-ca /etc/vastplan/pki/ca.crt \
  -nats-cert /etc/vastplan/pki/bootstrap.crt \
  -nats-key /etc/vastplan/pki/bootstrap.key \
  -nats-seed /secure/vastplan-nats/bootstrap.seed \
  -bootstrap -replicas 3 \
  -desired deploy/cluster.deployment.json
```

bootstrap seed 只在初始化/迁移作业中挂载，常驻 controller 和 node 不得持有。

## 5. 运行 Controller 与 Node Agent

Controller 使用独立 `controller-N.seed`：

```bash
go run ./kernels/backend controlplane -controller \
  -nats-url tls://nats.example.com:4222 \
  -nats-ca /etc/vastplan/pki/ca.crt \
  -nats-cert /etc/vastplan/pki/controller-1.crt \
  -nats-key /etc/vastplan/pki/controller-1.key \
  -nats-seed /secure/controller-1.seed \
  -key tenants.X2dsb2JhbA.states.Y2x1c3Rlcg
```

Node Agent 使用属于本节点的 `node-N.seed`：

```bash
bin/backend-kernel reconcile \
  -nats-url tls://nats.example.com:4222 \
  -nats-ca /etc/vastplan/pki/ca.crt \
  -nats-cert /etc/vastplan/pki/node-1.crt \
  -nats-key /etc/vastplan/pki/node-1.key \
  -nats-seed /secure/node-1.seed \
  -transport-seed /secure/node-1.transport.seed \
  -transport-trust /secure/transport-trust.json \
  -deployment cluster -node-id node-1
```

未显式配置 `-transport-seed` 与 `-transport-trust` 时，控制面 Node Agent 会拒绝启动；仅本地开发可以显式使用 `-nats-allow-insecure` 绕过 NATS 与 addressing 的生产门禁。传输信封会绑定 subject、payload、时间戳和 nonce，接收端据此校验签名并重建可信调用身份；JetStream 重投只跳过 nonce 重放检查，不跳过签名和身份校验。

## 6. 权限边界

| 角色 | 允许 | 明确禁止 |
|---|---|---|
| bootstrap | 创建/校准全部 KV 与发布初始配置 | 常驻业务进程持有 |
| controller | 读 Deployment/Node/Autoscaling Metric，写 Assignment/Controller Lease | 改 Stream、写 Actual/Capability/Metric |
| node | 读 Desired/Assignment，写 Actual/Node/Capability/Autoscaling Metric，RPC/事件 | 写 Deployment/Desired/Assignment |
| runtime | Capability、RPC、事件、写 Autoscaling Metric | 读取或改写部署控制面 |

新增 bucket 或 Subject 时，必须先修改 `RoleACL` 并增加真实权限允许/拒绝测试，不得在部署配置里临时放开 `>`。
