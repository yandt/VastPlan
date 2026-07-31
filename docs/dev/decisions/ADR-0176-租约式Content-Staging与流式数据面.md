# ADR-0176 租约式 Content Staging 与流式数据面

- 状态：已采纳，P2.4b 已实施
- 日期：2026-07-31
- 关联：[ADR-0172](ADR-0172-通用版本账本与可插拔存储Provider.md)、[ADR-0173](ADR-0173-版本环境与资源适配.md)、[ADR-0175](ADR-0175-统一版本生命周期与Resource-Adapter能力协商.md)

## 背景

Text、Blob 和 Files Adapter 都使用 `ContentFiles` 清单，但真实文件字节不能进入 JSON Capability Bus 或 Version Ledger。直接传输二进制会放大内存、消息上限、重试和集群转发成本；让插件自行选择本地目录、对象存储、上传 URL 或凭证又会绕过 tenant、资源绑定、安全准入和版本引用保护。

系统需要一条与 Workspace 生命周期衔接、但与具体存储 Provider 和传输协议解耦的数据路径。

## 决策

1. 建立中立控制契约 `version.staging.v1`。控制面只有 `beginUpload/uploadStatus/renewUpload/completeUpload/abortUpload`，不提供 `writeChunk`，也不携带文件字节、物理路径、Provider、endpoint、凭证、bearer token 或上传 URL。
2. `beginUpload` 只能由可信 Workspace 在解析 Environment/Resource 后发起。请求绑定精确 Workspace Session revision、Environment digest、Resource、逻辑相对路径、规范 mediaType、预期 SHA-256、大小和 Lease；tenant 与 actor 只来自可信 `CallContext`。
3. 文件字节使用独立流式数据面。浏览器经过同站认证 BFF，Backend/Runner 经过可信 Host streaming API；两者都以原调用身份和 Upload Lease ID 鉴权。插件不得监听任意上传端口或自带数据面 token。
4. Lease 状态为 `Pending/Uploading/Verifying/Ready/Rejected/Aborted/Expired`。revision 只围栏 renew、complete、abort 等控制转换，不按 chunk 增长；流式进度由 `receivedSize` 投影。complete 封闭输入并触发摘要、大小和安全准入，只有 Ready 才返回精确 ContentDescriptor。
5. Files Manifest 的每个 FileEntry 固定包含逻辑 path、SHA-256、size、mode 和规范 mediaType。它不保存 Upload Lease ID；同样内容在版本历史中的身份不受某次临时上传会话影响。
6. Workspace 接受 Files Manifest 前必须确认每个内容均属于同 tenant、Environment/Resource/Session 且已 Ready，摘要、大小和 mediaType 完全一致。文件名和 mediaType 只是声明，不能替代内容检测、安全扫描或压缩炸弹检查。
7. Ready 对象仍由临时 Lease 保护。Ledger commit 成功后，Workspace 通过持久 outbox 把精确内容摘要转为版本引用；引用确认前不得释放临时保护。失败重试不产生第二个逻辑版本，未提交或已中止对象在 Lease 与宽限期后回收。
8. Staging Manager 作为基础插件能力实现，不进入 Backend 内核。首个实现使用 Go 和本地内容寻址 File Provider；未来对象存储 Provider 通过内部端口替换，不改变 Workspace、Adapter 或业务插件协议。
9. Environment digest 必须固定内容存储路由和限制的受信修订。历史读取按原 digest 解析；缺少旧路由时失败关闭，不能退回当前 Provider 或猜测本地路径。
10. 单文件协议硬上限为 1 TiB，但具体 Environment、Provider、tenant 和资源类型必须设置更低配额。P2.4 不用协议硬上限代替磁盘容量、并发、速率、扫描和保留策略。

## 状态模型

```text
Pending ──stream──> Uploading ──complete──> Verifying ──> Ready
   │                    │                         └──────> Rejected
   ├──── abort ─────────┴───────────────────────────────> Aborted
   └──── lease expiry ─────────────────────────────────> Expired
```

## 备选方案

- **二进制直接进入 Capability Bus**：实现简单，但破坏有界消息、流式背压和低内存目标，拒绝。
- **业务插件直接上传对象存储**：生态成熟，但暴露 Provider/凭证并绕过 Workspace 绑定和统一准入，拒绝。
- **控制面返回预签名 URL**：远端对象存储常见，但会把 bearer 能力和 endpoint 带入通用契约；未来 Provider 可在可信数据面内部使用，协议层拒绝。
- **把 Upload Lease ID 写入版本清单**：会让临时会话身份污染不可变历史和去重，拒绝。

## 语言与运行方式

Go 适合首个 Manager/File Provider：流式 I/O、SHA-256、文件权限、原子 rename、并发限制和现有 Workspace/Ledger 集成成本最低。Rust 可作为未来高性能扫描或内容 Provider；Node/Python 更适合上层文本/媒体处理 Adapter，不承担首个可信暂存边界。运行方式为受管基础插件服务，不内嵌内核。

## 实施分解

1. **P2.4a（已完成）**：冻结 `version.staging.v1` Go DTO、JSON Schema、严格 Wire、错误码、Lease 状态和 FileEntry mediaType。
2. **P2.4b（已完成）**：实现 Go Staging Manager、本地内容寻址 File Provider、`io.Reader` 流式写入、原子完成、启动/周期租约回收和文件/tenant/服务/并发容量配额。控制面只接受可信 Workspace，begin 通过 `CallContext.idempotencyKey` 防止响应丢失后重复预留；本地 Provider 使用 tenant 哈希分区、私有权限、严格持久状态、CAS 硬链接发布和 Ready 启动复核。内置 Admission 只提供完整性基线，不冒充恶意软件扫描。
3. **P2.4c**：Workspace 接入 begin/complete/manifest 验证、临时保护转版本引用 outbox，并实现 Text/Blob/Files Adapter。
4. **P2.4d**：增加浏览器 BFF 与 Backend/Runner streaming SDK、对象存储 Provider、安全扫描准入和跨节点故障矩阵。

## 实施记录：P2.4c1（2026-07-31）

P2.4c 拆为两个可独立失败关闭的子阶段。P2.4c1 已完成 Workspace begin/status/renew/complete/abort 内容上传代理，并按 Session revision 记录 Ready 绑定。Files Manifest 只接受同 tenant、Session、Environment、Resource、path、digest、size、mediaType 且 Lease 未过期的内容，或基线/当前已验证候选中仍受保护的相同内容。

同时实现 `text.v1/blob.v1/files.v1` 标准 Adapter 和文件级确定性 diff。P2.4c1 显式阻止 Files commit，避免 durable reference 尚未建立时产生可提交但随后丢失对象的历史。P2.4c2 再增加提交前持久保护、Ledger 提交、版本引用确认与响应丢失恢复 outbox，完成后开放 Files commit。

## 实施记录：P2.4c2（2026-07-31）

P2.4c2 已完成。新增中立 `version.content-reference.v1`，并由 Content Staging 插件以第二项集群能力 `foundation.versioning.content-reference` 提供；既有 `version.staging.v1` 上传协议保持不变。版本内容保护记录和 CAS 对象由同一 Provider 持久化，避免把 outbox 放入 Workspace 内存或另建第二套存储真相。

Files commit 固定执行以下事务链：

1. Workspace 冻结规范化 Manifest，以稳定 `operationId` 调用 `prepareVersion`；Manager 校验每个新条目来自同 tenant、Session、Environment、Resource 和内容身份完全一致的 Ready Upload，已被确认版本保护的 CAS 对象可省略临时 Upload ID；
2. Content Staging 先原子持久化 `Prepared` 保护，再允许 Workspace 调用幂等 `Ledger.PutVersion`；
3. Ledger 返回精确 `VersionRef` 后，Workspace 调用 `confirmVersion`，要求 stream 和 `contentDigest` 与准备的 Manifest 完全一致；确认记录不再随 Upload Lease 或 Prepared 时限过期；
4. 内容确认完成后才执行可选 Head create/move。Head 冲突会留下可追溯的 detached version，不回滚不可变版本或已确认内容；
5. 任何模糊错误均不自动 abort。调用方以同一 `operationId` 重试，prepare、Ledger 写入和 confirm 都幂等；确认响应丢失不会产生第二个版本。

`Prepared` 是有界崩溃保护，不是永久垃圾保留：每次有效重试会续展保护，超过配置时限仍未确认则进入 `Expired`，待 Upload Lease 和其他保护均消失后回收 CAS 对象。过期后仍可用同一 `operationId` 和完全相同的 Manifest 重新上传缺失内容并恢复 Prepared，不会创建第二个 Ledger 版本。`Confirmed` 当前永久保留；未来只有在 Version Ledger 增加明确的版本删除/保留策略后，才可通过单独的 release 协议减少引用，不能根据 Head 移动推断版本已无用。

Workspace Session 仍是临时编辑区，不引入持久 Session 状态。跨 Leader 恢复通过“调用方重开 Session、重新上传缺失候选、复用同一领域 `operationId`”完成；Content Reference 的逻辑幂等比较忽略新的 Session 和临时 Upload ID，但严格固定 tenant、actor、Environment、Resource、stream、Manifest 摘要和全部内容描述。
