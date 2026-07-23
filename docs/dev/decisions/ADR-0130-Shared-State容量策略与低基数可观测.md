# ADR-0130 Shared State 容量策略与低基数可观测

- 状态：已采纳，代码已实现；企业容量基线待 A3 实测
- 日期：2026-07-23

## 背景

Shared State 已成为五个关键平台插件的共同状态真源，但 `VASTPLAN_SHARED_STATE_V1` 此前没有 `MaxBytes`，长期写入和 Credentials 不可变 chunk 会无限占用 JetStream 磁盘。NATS 自带 stream bytes 真值，却不理解平台 warning/critical 门槛；Shared State HostService 也没有稳定的 CAS 冲突、不可用和操作时延指标。把 tenant、plugin ID 或 key 放进指标又会造成高基数和敏感身份泄漏。

## 决策

1. Shared State backing stream 必须配置正数 `MaxBytes`。KV 固有的 `DiscardNew` 继续作为硬门禁：达到上限拒绝新写，不淘汰旧 revision，不自动运行 GC，也不回退 File Provider。
2. 生产 bootstrap 必须显式提供 `shared-state-max-bytes`；允许范围为 64 MiB 到 1 PiB。没有真实负载证据时内核不猜生产容量。本地 `nats-allow-insecure` 可使用明确的 1 GiB 开发上限。
3. warning/critical 满足 `1 <= warning < critical < 100`，默认 70%/85%。策略与 schema version 写入同一 stream metadata；采样器以及 Backend `OpenBuckets` 缺少 metadata、硬上限或发现非法阈值时都 fail-closed，不允许运行实例连接旧的 unlimited stream，也不自行采用本地默认。
4. 容量状态固定为 `ready/warning/critical/full`。快照只返回总 used/max/available bytes、usage basis points、历史消息总数、每 key 历史上限、存储类型和压缩状态，不返回 tenant、RuntimeScope、plugin、namespace、key 或 value。
5. `sharedstatectl capacity` 使用只读 backup NATS 身份读取 stream 真值，默认在 critical/full 返回非零；可配置 `warning|critical|full|none` 作为外部监控退出门槛。当前不新增常驻采集进程，企业 Prometheus/OTel 适配器可消费相同 JSON/Go 快照。
6. 每个 Runtime Host 使用已有 Observer 记录 `shared_state_operations_total` 与 `shared_state_operation_duration`。标签只有闭合 operation `get/create/update/delete/list` 和 outcome `ok/not_found/conflict/invalid/unavailable/identity_rejected/encoding_error`；指标所属运行单元由宿主诊断边界表达，不重复增加业务身份标签。
7. A2 只治理整个 Shared State bucket。按 tenant/plugin 的硬配额需要可信身份聚合、突发额度和跨 key 原子 reservation；在真实容量分布出现前不扫描正文、不从物理 key 建高基数指标，也不伪装已经实现。
8. 核心、工具和 Provider 适配继续使用 Go，不增加插件或进程。原因是 JetStream config/status、Host Observer 与生产启动入口均在 Go；Node/Python 重写不会增加生态能力，只会复制安全策略。

## 备选方案

- **仅依赖 NATS 通用监控**：没有平台阈值、Provider outcome 和 fail-closed 配置校验，不采用。
- **每个插件自行上报容量**：重复实现且容易泄漏 tenant/key，拒绝。
- **达到上限自动删除历史或孤儿 chunk**：可能破坏回滚和并发 writer，拒绝；A4 在备份恢复和 writer fencing 之上单独实现。
- **硬编码统一生产上限**：不同企业规模差异过大，改为生产显式配置。
- **立即提供 tenant/plugin 配额**：缺少真实分布与 reservation 语义，暂缓。

## 影响

正面：磁盘增长有明确上界；阈值是 stream 真源；满载时旧状态仍可读；容量和操作指标低基数且不泄漏业务身份；外部监控不需要持有 bootstrap 权限。

代价：生产部署升级时必须选择容量并重新运行 bootstrap 校准 metadata；达到 full 后业务写入会 fail-closed，需要扩容或受控 GC；总量快照暂时不能指出具体租户贡献。

## 验证

- 生产启动缺少硬上限拒绝，本地启动得到明确 1 GiB 上限；
- 自定义 max/warning/critical 进入 JetStream config/metadata，stream 使用 `DiscardNew`；
- ready/warning/critical/full 边界和监控退出等级闭合；
- Host 指标覆盖成功、CAS conflict、not found 等结果，metric key 不含 tenant/plugin/key/value；
- 全仓 Go、race、架构守护和发布门禁通过；真实容量增长、告警时延和扩容恢复进入 A3。
