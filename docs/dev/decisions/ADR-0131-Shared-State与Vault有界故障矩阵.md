# ADR-0131：Shared State 与 Vault 有界故障矩阵

- 状态：已采纳；本地自动化已实现，企业网络分区与 SLA 验收待目标环境执行
- 日期：2026-07-23

## 背景

Shared State 已具备 JetStream 三副本配置、CAS、签名备份恢复、容量策略和低基数指标，但此前“集群测试”主要验证三台 Backend Node 的调度与插件 leader，底层 NATS 仍是单 Server。Credentials 的 Vault Transit 也只有普通功能桩，不能证明仲裁丢失、重连、Vault 超时和解密恢复时会 fail-closed。

本阶段明确不做 soak。目标是有限、可重复、会主动制造故障的正确性矩阵；长期稳定性测试继续等待代表性插件负载。

## 决策

采用两层混合验证，而不是只选进程内测试或只选 Docker：

1. 仓库自动化启动三个真实 `nats-server` 实例、独立 FileStore 和三副本 KV，验证 stream leader 故障、失去多数派、节点原目录重启、客户端重连、revision 单调性和旧 writer fencing。该测试进入显式 E2E，且由 `shared-state-fault-matrix.sh` 单独运行。
2. Vault 使用真实 HTTP 传输边界的故障服务，覆盖 403、畸形响应、客户端超时、恢复后 decrypt，以及 Material Lease 在 Vault 不可用时拒绝且不返回明文或密文。decrypt 与 retire 的竞态继续由确定性的阻塞 Transit 测试复核。
3. 真实服务器/容器网络分区、磁盘满、进程 `SIGKILL`、Vault HA 切主以及生产 RPO/RTO 不伪装成本地测试结果，必须在目标企业环境按本 ADR 的验收表执行。
4. 测试必须有总超时、只绑定 `127.0.0.1`、使用临时目录并自动回收；不得操作开发者已有 NATS 或 Vault。

## 超时写入语义

失去多数派时 Shared State 写调用必须向调用方返回失败，且绝不回退本地 File Store。但分布式提交存在“不确定结果”：请求可能已进入 Raft 日志，调用方先收到超时，恢复多数派后该 CAS 仍可能提交。

因此调用方不得把超时直接解释为“确认未写入”，也不得盲目重放非幂等操作。恢复后必须读取权威 key 与 revision，按业务操作 ID/期望 revision 对账，再决定重试。无论超时请求是否最终提交，旧 revision 都必须被 fencing。

## 自动化验收矩阵

| 场景 | 自动化断言 |
|---|---|
| Stream leader 停止 | 剩余两节点继续读写，revision 增长，旧 revision 冲突 |
| 再停止一个节点 | 写调用超时/失败，不返回伪成功，不创建本地旁路状态 |
| 两节点按原存储目录重启 | 三副本全部追平，客户端恢复，状态不倒退，CAS 继续单调 |
| Vault 403/畸形响应/超时 | decrypt fail-closed，错误不含 token、明文或 ciphertext |
| Vault 恢复 | decrypt 与 Material Lease 恢复，lease 仅目标可信宿主可解封 |
| decrypt 期间凭证退役 | 重新读取 Credentials Root，退役胜出，拒绝签发 lease |

## 企业环境验收边界

目标环境至少执行：

- 隔离一个 NATS 节点的双向 route 流量，确认少数派不能提交，健康多数派继续服务；
- 隔离两个节点，确认所有写入 fail-closed；恢复网络后核对 stream replica `current=true`、`lag=0` 和业务 revision；
- 对 NATS leader、Vault active 节点执行非优雅终止并测量恢复时间；
- 执行一次停写、签名备份、空目标三节点恢复，记录实际 RPO/RTO；
- 注入磁盘容量 warning/critical/full，验证告警、拒写和扩容恢复；
- 保存时间、版本、拓扑、故障命令、观察指标和结果摘要，不保存 Shared State value 或凭证明文。

本地毫秒数据只能作为回归参考，不能宣称企业 SLA 已达标。默认运维目标仍是 RPO 不超过一小时、RTO 不超过两小时，最终值由企业部署要求收紧。

## 影响

- 优点：日常可以稳定发现“单节点伪集群”、CAS 回退、Vault fail-open 和恢复后 revision 倒退；环境验收与代码门禁职责清楚。
- 代价：真实网络分区与 HA Vault 仍需部署环境；进程内 NATS 时间不能代表生产恢复时间。
- 后续：完成企业环境 A3 记录后再标记生产验收完成；soak 仍按既定决定推迟。
