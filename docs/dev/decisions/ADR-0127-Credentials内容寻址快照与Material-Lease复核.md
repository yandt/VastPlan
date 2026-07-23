# ADR-0127 Credentials 内容寻址快照与 Material Lease 复核

- 状态：已采纳，代码、签名备份恢复与本地有界故障矩阵已实现；企业环境验收待执行
- 日期：2026-07-23

## 背景

Credentials 0.10.0 把所有租户的命名凭证、托管凭证密文、生命周期审计和维护状态原子写入单机 JSON。普通插件迁移使用的“每租户单 Shared State 值”不适合凭证：单个 Vault Transit ciphertext 就可能超过 1 MiB，审计也会持续增长。把记录、审计序号和维护统计直接拆成多个可变 key，则会在 Shared State v1 缺少跨 key 事务时产生“状态已激活但审计未提交”的安全窗口。

## 决策

1. 每个租户使用一个小型 `credentials.snapshot.v1` Root 作为唯一可变 CAS 提交点。Root 只包含总大小、完整摘要和有序 chunk 摘要，不包含凭证名称、handle、密文或审计内容。
2. 租户完整安全快照使用规范 JSON 编码，按不超过 512 KiB 切分。chunk 以 SHA-256 内容寻址并 create-only 保存；读取时逐块校验 key、大小、块摘要、总大小和总摘要。
3. 写入顺序固定为“写完并复核所有不可变 chunk → CAS 创建/切换 Root”。因此崩溃时读者只能看见完整旧快照或完整新快照；stale leader 的 Root CAS 失败，不能覆盖新状态。
4. 当前不在线删除 chunk。无租约 GC 可能删除另一个尚未提交 Root 的 writer 正在使用的 chunk；内容寻址可以去重，失败提交产生的孤儿块留待具备 writer lease/fencing 的生产容量控制器处理。备份必须同时覆盖 Root 和全部 chunk。
5. Material Lease 在 Vault decrypt 前读取目标记录，decrypt 后重新读取最新 Root 和完整快照，再次核对 tenant、handle、完整 Ref、状态和 ciphertext。任何变化都拒绝签发，明文随后立即清零。
6. Credentials 先保持 `leader + external-shared + leader routing`。Vault Transit encrypt/rewrap 是返回新 ciphertext 的转换，但维护、审计与失效语义尚未完成双实例故障演练；通过 stale writer、解密竞态、跨节点接管和备份恢复验收后再评估 active-active。
7. Shared State tenant scope 不能由后台任务自报 tenant。过期维护改为租户请求触发的有界 lazy maintenance；Preparing 的安全判定在任何 Material Lease/生命周期入口前执行。未来如需无请求租户的准时清理，由可信内核提供 tenant-scoped scheduler，不能让插件保存或伪造用户 CallContext。
8. 当前处于开发阶段，不迁移旧文件；删除 `VASTPLAN_CREDENTIALS_STATE_FILE`。生产已有历史时必须另做冻结、摘要核对和可回滚导入工具。
9. 插件继续使用 Go 与现有可信独立进程。Vault/KMS、内存清零、并发限流和宿主契约已在 Go 实现；Node/Python 重写不会带来生态收益，反而扩大 material 处理面。

## 备选方案

- **每租户一个 Shared State value**：无法容纳大 ciphertext 和长期审计，拒绝。
- **每条记录独立 CAS + 审计 outbox**：读写扩展性更高，但需要跨 key 恢复协议、全局序号分配和孤儿密文 GC；在 v1 阶段增加安全状态数量，暂不采用。
- **凭证插件直接连接 NATS/SQL**：扩散基础设施凭证与租户隔离逻辑，拒绝。
- **把 ciphertext 存进 Vault KV**：可作为未来 Secret Storage Provider，但会新增自举、HA、授权和迁移依赖；Transit 当前只负责密码学转换，不混入存储职责。

## 影响

正面：保持现有凭证记录、审计与维护的单事务语义；支持超过 1 MiB 的租户状态；跨节点新 Leader 可接管；Root 不泄漏业务标识；Material Lease 能发现跨实例失效竞态。

代价：凭证写入需要重新编码租户快照，适合低频安全控制面而非高频数据面；失败提交可能遗留不可变 chunk；租户长期没有请求时只延迟清理 Aborted 历史，不影响 Candidate/Active 的即时状态校验。

## 验收

- 多 chunk 快照重启读取、摘要篡改拒绝和 tenant 隔离；
- 两个实例从同一 Root 更新时只有一个 CAS 成功；
- Root 切换前故障仍读取旧快照，切换后缺块 fail-closed；
- revoke/retire 与 Vault decrypt 竞态不会签发 Material Lease；
- Provider 不可用时不回退本地文件；
- 全量 Go、race、真实 Portal 生命周期和发布门禁通过。
- [ADR-0129](ADR-0129-Shared-State签名备份与空目标恢复.md) 已增加 schema owner 维护的 Credentials 备份验证器：备份和恢复后都复核 Root、每个引用 chunk 及完整拼接摘要；孤儿 chunk 只计数，不在无 writer lease 的备份流程中删除。
- [ADR-0131](ADR-0131-Shared-State与Vault有界故障矩阵.md) 已覆盖 Vault HTTP 403、畸形响应、超时与恢复，Material Lease 在 Vault 故障时 fail-closed，以及 decrypt/retire 竞态；真实 Vault HA 切主仍属企业环境验收。
- [ADR-0132](ADR-0132-Credentials孤儿Chunk安全回收.md) 已补齐 host-only Leader evidence 约束的 fenced Shared State mutation，以及 schema owner 内部的分页、两阶段宽限、Root 复核和崩溃恢复 GC。
- [ADR-0133](ADR-0133-Credentials保持Leader与Active-Active前置条件.md) 已完成 A5 评估：一次性 Authority consume 与 Root CAS 之间缺少 durable command/outbox，GC 需要唯一 fenced owner，因此当前正式保持 active-passive。
