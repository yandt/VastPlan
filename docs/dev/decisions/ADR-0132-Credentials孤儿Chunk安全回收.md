# ADR-0132：Credentials 孤儿 Chunk 安全回收

- 状态：已采纳；代码、三语言 SDK、备份验证与自动化门禁已实现
- 日期：2026-07-23

## 背景

Credentials 使用“小型 Root CAS + 不可变内容寻址 chunk”保存每租户完整安全快照。该顺序保证读者只能看到完整旧快照或完整新快照，但 writer 在 Root CAS 前崩溃、CAS 冲突或失去领导权时会留下未被 Root 引用的 chunk。Shared State 已有总容量硬上限，长期不回收会最终令安全控制面拒写。

直接扫描后删除不可达 chunk 不安全：另一个 writer 可能已经上传 chunk、尚未提交 Root；失去领导权的旧进程也不能继续拥有写入或删除资格。GC 还必须能从 marker 已写、blob 已删、控制器游标未推进等任意崩溃点恢复。

## 决策

1. 不建立独立 GC 插件。Credentials 是 `credentials.ledger` 的唯一 schema owner，只有它能解释 Root 可达性；把格式和广泛命名空间权限复制给另一插件会形成第二真相源。
2. Shared State 增加独立的 `kernel.state.shared.fenced.create/update/delete` 能力。请求结构与普通 `state.shared.v1` 相同，但可信 Host 必须同时验证 RuntimeIdentity、当前 Unit Leadership evidence，且 evidence 的 UnitID 必须等于 RuntimeScope。epoch/token 只在宿主 context 中存在，不进入插件、CallContext、payload 或日志。
3. 普通 `kernel.state.shared.create/update/delete` 保持不变，供 Active-Active + CAS 插件使用。插件必须在签名清单中精确声明 fenced 能力；无 Leader evidence、证据属于其他 Unit 或 lease 已失效时统一 fail-closed 为 `state.fence_invalid`。
4. Credentials 0.12.0 的 Root、chunk、GC state、marker 和删除全部使用 fenced writer；读取和分页扫描继续使用普通 get/list。进程内 `workflowMu` 串行当前租户工作流，host fence 排除交接后的旧 Leader。
5. GC 采用可恢复的 `mark -> sweep -> idle` 状态机。每租户 `gc.state` 只保存阶段、分页 cursor、周期时间和低敏计数；每个候选使用 `gc.marker.<digest>` 保存 blob revision 与首次不可达时间。状态不保存 tenant、凭证名、handle、ciphertext 或 caller。
6. 每次受信租户请求最多推进一页，默认 100、上限 200。没有进行中的周期时按维护 interval 启动；因此不需要插件自报 tenant，也不增加常驻后台进程。未来 tenant-scoped scheduler 可以调用同一个维护入口，而不改变 GC 格式。
7. mark 只为当前 Root 不可达且 key/content SHA-256 一致的 blob 创建 marker。重复 mark 幂等；若同 digest blob revision 变化，重置首次观察时间，不能沿用旧宽限期。
8. sweep 默认要求 marker 连续存在至少 24 小时，可配置范围 1 小时至 30 天。删除前必须重新读取权威 Root、重新读取 blob、核对 key/digest/revision，并使用 expected revision 删除。重新变为可达的 chunk 只删除 marker，不删除 blob。
9. 删除 blob 后、删除 marker 前崩溃是允许状态；下次 sweep 发现 blob 不存在后清理 marker。marker、控制器状态和 orphan chunk 一并进入签名备份；恢复验证器严格解析 GC 元数据，但允许 marker 暂时指向已删除 blob，因为这是可恢复提交点。
10. GC 失败发生在业务 work 之前并终止本次请求，避免业务已经提交却因维护失败返回模糊结果。已完成的 mark/delete 保持幂等，不回滚到本地文件，也不删除任何 Root 可达内容。
11. 实现继续使用 Go，并留在现有 Credentials 可信独立进程。原因是 Root schema、Shared State SDK、Leader Host evidence、Vault material 边界和备份验证器都在 Go；Node/Python 不提供额外生态收益，反而扩大安全状态面。

## 备选方案

- **无宽限期扫描删除**：可能删除尚未提交 Root 的 writer chunk，拒绝。
- **只依赖进程内互斥锁**：不能约束失去 lease 的旧进程，拒绝。
- **把 epoch/token 写入插件 payload 或 Root**：会暴露 bearer-like 证据并复制选主真源，拒绝。
- **内核直接理解 Credentials Root 并回收**：污染微内核业务边界，拒绝。
- **立即增加 tenant-scoped 常驻调度器**：可以覆盖无请求租户，但当前并非 GC 正确性的必要条件；先复用租户请求触发，未来按通用调度能力单独设计。

## 影响

正面：Shared State 容量可以受控恢复；旧 Leader 不能继续写 Root/chunk 或执行删除；GC 有宽限、Root 复核、CAS 删除和崩溃恢复；内核新增能力保持通用且不理解凭证格式。

代价：冷租户在没有请求时不会主动推进 GC；每个 orphan 暂时增加一个小 marker；GC 最少需要 mark 与 sweep 两次请求，分页较多时需要更多请求。达到 Shared State full 后若连 marker 都无法创建，运维仍需先扩容，不能靠无空间 GC 自救。

## 验收

- 缺少、错误或已失效 Leader evidence 时 fenced mutation 拒绝，普通 CAS 写入不受影响；
- Root 切换产生的旧 chunk 经 mark、宽限和 sweep 后删除，当前 Root 仍可完整读取；
- mark 后重新被 Root 引用的 digest 不删除；
- blob 已删、marker 未删的崩溃点可恢复；
- marker/blob revision 变化重置宽限期；
- 每轮处理数不超过配置上限，cursor 可跨请求恢复；
- 备份和恢复验证同时接受合法 GC 中间态并继续拒绝未知/损坏 key；
- 全量 Go、race、Shared State 故障矩阵、Credentials E2E 和发布门禁通过。
