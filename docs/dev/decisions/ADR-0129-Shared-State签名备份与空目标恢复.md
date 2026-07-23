# ADR-0129 Shared State 签名备份与空目标恢复

- 状态：已采纳，代码已实现；生产 RPO/RTO 实测并入故障矩阵阶段
- 日期：2026-07-23

## 背景

Global Settings、Plugin Settings、Portal Composer、Deployment Manager 和 Credentials 已把跨实例状态迁入 `VASTPLAN_SHARED_STATE_V1`。JetStream 三副本能处理单节点故障，却不能代替误删、逻辑损坏、集群整体丢失和跨故障域备份。逐 KV 导出重新写入后会重编号 JetStream sequence/revision，破坏现有调用方持有的 CAS revision；仅保存原生 snapshot 又无法证明 Credentials Root 引用的全部内容寻址 chunk 完整。

## 决策

1. JetStream backing stream `KV_VASTPLAN_SHARED_STATE_V1` 的原生 snapshot 是唯一权威恢复载荷。逐 KV JSON 不作为恢复格式，也不把 ciphertext、配置正文或租户 key 写入备份清单。
2. 备份工具先扫描当前 KV，严格解析物理身份、计算包含 key/revision/value digest 的整体逻辑摘要，并运行领域验证器。首个验证器属于 Credentials schema owner，复核每个 Root、引用 chunk 的 key/大小/内容摘要和完整拼接摘要；孤儿 chunk 只计数，不在备份时删除。
3. 在线备份使用乐观静默窗口：记录 stream state，完成逻辑扫描与 `jsck` 原生 snapshot，再复核前后 `messages/bytes/firstSeq/lastSeq`。任何写入使整次尝试作废并从头重试；持续写入时明确失败，不输出弱一致备份。
4. 归档固定包含 `stream.snapshot`、`manifest.json` 和 `manifest.sig.json`，目录为 `0700`、文件为 `0600`，使用同目录临时目录、fsync 和原子 rename 提交。manifest 只记录流配置/状态、整体摘要、计数和验证结果，不列出物理 key 或正文。
5. manifest 必须由独立 Ed25519 备份签名密钥签名。NATS 登录 seed、transport seed、制品签名密钥和备份签名私钥不得复用；恢复端只持有允许轮换的公开信任文档。
6. NATS 增加 `shared-state-backup` 与 `shared-state-restore` 两个不同身份。前者只能读取 Shared State 和请求 snapshot；后者只能读取以便复核、请求 restore 并向服务端分配的精确 restore subject 发送 chunk。二者都不能发布 `$KV.*` 业务写入，不能访问 Deployment、Desired 或其他控制面。
7. restore 只允许目标 backing stream 不存在。禁止原地覆盖、merge、逻辑 upsert 或自动删除现有流；这同时避免恢复数据与运行 writer 发生 revision 分叉。操作者还必须提交已验签 manifest 的完整 SHA-256，并显式确认所有 writer 已停写、旧集群入口和身份已撤销。
8. 恢复完成后重新读取全部最新 KV，复算逻辑摘要、领域验证结果及 stream state。任何差异都 fail-closed，服务不得启动；工具不自动删除失败目标，须在变更控制下调查、删除空目标并重试。
9. 软件无法从目标集群证明旧集群不会重新联网。停流量、停止 Backend/Node、撤销旧 NKey、隔离旧 NATS 和确认单一恢复目标属于强制运维前置条件，不能用一个分布式锁伪装为已解决跨灾难域 split-brain。
10. 首个默认运维目标为每小时一次签名备份、重要变更前额外备份、RPO 不超过一小时、RTO 目标两小时、异地不可变保存和至少季度恢复演练。该值是上线起点，不是性能证明；A3 必须在企业三节点 NATS/Vault 环境测量并按 SLA 收紧。

## 备选方案

- **只做逐 KV 逻辑导出/导入**：可读性高，但 revision 重编号，拒绝作为恢复载荷。
- **只保存原生 snapshot**：revision 正确，却缺少领域完整性证明与安全清单，拒绝单独使用。
- **在线冻结所有插件写入后备份**：一致性直观，但每小时制造平台写停顿；当前原生 snapshot 加序列稳定窗口已能获得一致点，不作为默认。
- **恢复到已存在 bucket 并覆盖**：会形成历史、CAS 和运行实例分叉，拒绝。
- **备份即回收 Credentials 孤儿 chunk**：writer 可能尚未提交 Root；回收留给具备 lease/fencing 的 A4 控制器。

## 影响

正面：完整保留 JetStream sequence/revision 和历史；备份不泄漏正文；Credentials 在备份与恢复两端都经过引用完整性验证；签名和最小身份降低归档替换与操作越权风险。

代价：File/Memory Store 不是生产备份源；持续高频写入时备份会重试或失败；恢复必须是受控停机流程；原生 snapshot 与 NATS Server 兼容性需要纳入升级前恢复演练。

## 验证

- 签名归档可从 FileStorage KV 原生 snapshot 恢复；
- Root/chunk、普通插件状态、历史 revision 和恢复后 CAS 连续性保持；
- 篡改 snapshot、错误签名、错误确认摘要、未确认停写和非空目标均拒绝；
- backup/restore NATS 身份可读取复核但不能 `$KV` 写入，并且 snapshot/restore API 相互隔离；
- 全量 Go、race、发布门禁与后续真实三节点故障矩阵共同验收。
