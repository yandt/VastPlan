# Linux 节点 SSH 首次引导

本文说明如何用生产入口把一台 Linux/systemd 主机纳入 VastPlan。SSH 只用于首次安装；完成后由 systemd 托管 Node Agent，后续部署通过控制面 reconcile。

## 1. 前置条件

- 目标主机具有 `systemd`、`curl`、`sha256sum`、`base64`、`getent`、`groupadd`、`useradd`、`usermod`、`install` 和 `mktemp`；
- 引导用户使用密钥登录，且只在受控审批期间允许无交互执行固定的 `sudo -n /bin/sh -s --`；
- 主机 SSH 公钥已由带外渠道核验并写入专用 `known_hosts`，禁止首次连接自动信任；
- Backend Linux 制品已经在企业准入流水线验证来源证明和 SBOM，并发布到内部 HTTPS 地址；
- 已为节点签发独立 NATS mTLS、NKey 和 addressing transport 身份；
- 远端插件仓库、发布者信任文档与只读 token 已准备完成。

所有本地 request、SSH identity 和待下发文件必须是仅属主可访问的普通文件。远端文件目标只能使用 `/etc/vastplan/secrets/` 下的安全单层文件名，mode 固定为 `0440`；目录为 `root:vastplan 0750`，Node Agent 只能读取，不能替换凭证。

## 2. 引导请求

建立一个权限为 `0600` 的 `node-a.bootstrap.json`：

```json
{
  "target": {"address":"node-a.internal","port":22,"user":"bootstrap"},
  "release": {
    "version":"1.0.0",
    "url":"https://releases.internal/vastplan/backend-kernel-linux-amd64",
    "sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "node": {
    "id":"node-a",
    "tenant":"acme",
    "deployment":"production",
    "labels":"region=cn,tier=platform",
    "natsUrl":"tls://nats.internal:4222",
    "natsCa":"/etc/vastplan/secrets/nats-ca.pem",
    "natsCert":"/etc/vastplan/secrets/node.crt",
    "natsKey":"/etc/vastplan/secrets/node.key",
    "natsSeed":"/etc/vastplan/secrets/node.seed",
    "transportSeed":"/etc/vastplan/secrets/transport.seed",
    "transportTrust":"/etc/vastplan/secrets/transport-trust.json",
    "repositoryUrl":"https://artifacts.internal",
    "repositoryCa":"/etc/vastplan/secrets/artifact-ca.pem",
    "repositoryTrust":"/etc/vastplan/secrets/artifact-trust.json",
    "capacityCpuMillis":4000,
    "capacityMemoryBytes":8589934592,
    "capacityGpu":0
  },
  "secretFiles": [
    {"source":"/secure/node-a/nats-ca.pem","destination":"/etc/vastplan/secrets/nats-ca.pem","mode":288},
    {"source":"/secure/node-a/node.crt","destination":"/etc/vastplan/secrets/node.crt","mode":288},
    {"source":"/secure/node-a/node.key","destination":"/etc/vastplan/secrets/node.key","mode":288},
    {"source":"/secure/node-a/node.seed","destination":"/etc/vastplan/secrets/node.seed","mode":288},
    {"source":"/secure/node-a/transport.seed","destination":"/etc/vastplan/secrets/transport.seed","mode":288},
    {"source":"/secure/node-a/transport-trust.json","destination":"/etc/vastplan/secrets/transport-trust.json","mode":288},
    {"source":"/secure/node-a/artifact-ca.pem","destination":"/etc/vastplan/secrets/artifact-ca.pem","mode":288},
    {"source":"/secure/node-a/artifact-trust.json","destination":"/etc/vastplan/secrets/artifact-trust.json","mode":288},
    {"source":"/secure/node-a/artifact.env","destination":"/etc/vastplan/secrets/artifact.env","mode":288}
  ]
}
```

JSON mode `288` 等于八进制 `0440`。`artifact.env` 只能包含一行：

```text
VASTPLAN_ARTIFACT_READ_TOKEN=<至少16位的受控令牌>
```

## 3. 执行

```bash
chmod 0600 node-a.bootstrap.json /secure/ssh/node-bootstrap.key /secure/node-a/*

./bin/backend-kernel node-bootstrap \
  -request node-a.bootstrap.json \
  -identity /secure/ssh/node-bootstrap.key \
  -known-hosts /secure/ssh/known_hosts \
  -timeout 5m
```

成功输出只包含节点、SSH endpoint、systemd 服务名和 `systemd_active`，不回传秘密或远端日志。随后必须在控制面确认：

1. `node-a` 的 Node Lease 出现且传输身份匹配；
2. Controller 为 `acme/production/node-a` 发布 Assignment；
3. ActualState 收敛到目标 revision；
4. 所有 unit 为 active，没有候选失败或持续重启。

在 Node Lease 出现前，不得把 SSH 成功等同于集群接管完成。

### systemd 就绪、看门狗与升级

新生成的 unit 使用 `Type=notify`：Node Agent 只有在接入控制面并启动 Node Lease Guard 后才发送 `READY=1`。`WatchdogSec=60s` 只在 Agent/Reconciler 控制循环真实推进，或处于与 reconcile deadline 相同的 15 分钟有界工作租约时收到存活通知；普通卡死会快速触发失败重启，长任务不会被 60 秒阈值误杀，超过租约仍未返回则停止喂狗。`KillMode=control-group` 会在服务退出时回收 Kernel、Runtime Host、独立插件及其子孙进程。完整分层见 [ADR-0094](../decisions/ADR-0094-操作系统Guardian与独立进程故障收敛.md)。

已经安装的旧 unit 不会因 Kernel 二进制升级自动获得这些 systemd 属性。升级到本决策后的首个版本时，必须通过受控引导/升级流程原子重装 unit、执行 `systemctl daemon-reload` 并重启服务；随后检查 `systemctl show vastplan-node-agent.service` 中 `Type=notify`、`WatchdogUSec=1min` 和 `KillMode=control-group` 已生效。

## 4. 失败与重试

- host key 不匹配：按主机身份事件处理，禁止使用跳过校验参数；
- SHA-256 不匹配：隔离内核制品，禁止从其他节点复制替代；
- systemd 启动失败：从目标主机的受控 journal 和支持包诊断，平台错误只保留稳定码；
- 重试同一请求是幂等的：版本目录、秘密文件和 unit 都原子替换；
- 接管完成后撤销引导账户或收回其 sudo 授权，日常升级通过 Node Agent/控制面执行。
