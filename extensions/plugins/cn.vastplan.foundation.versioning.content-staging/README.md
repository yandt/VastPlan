# Version Content Staging

插件 ID：`cn.vastplan.foundation.versioning.content-staging`

该基础插件在 Version Workspace 与真实文件字节之间提供受信暂存边界。它管理短时 Upload Lease、流式摘要与大小校验、tenant 配额、本地内容寻址对象和过期回收；它不把文件字节、物理路径、存储 Provider、endpoint、凭据、token 或上传 URL 放进 JSON Capability Bus。

## 当前能力

- `version.staging.v1` 控制面：`beginUpload/uploadStatus/renewUpload/completeUpload/abortUpload`，`beginUpload` 强制使用可信 `CallContext` 的 idempotency key 防止响应丢失后重复预留；
- 仅接受 `cn.vastplan.foundation.versioning.workspace` 的受信控制调用，并从 `CallContext` 取得 tenant 与 subject；
- `Manager.Stream(ctx, scope, uploadID, io.Reader)` 是独立 Go 数据面端口，按 128 KiB 缓冲流式读取，不在内存中聚合整个文件；
- `Pending/Uploading/Verifying/Ready/Rejected/Aborted/Expired` 状态机，控制转换使用 revision CAS，流式进度不增加 revision；
- 单文件、tenant 总量、服务总量和 tenant 活动上传数四级配额；
- 本地 File Provider 使用 tenant 哈希分区、权限 `0700/0600`、严格状态 JSON、SHA-256 CAS 对象、硬链接原子发布和目录 fsync；
- 启动恢复会复核全部 Ready 对象，缺失或摘要不符时失败关闭；
- 后台周期回收和启动回收会清理过期 Lease、无引用临时对象及超过保留期的终态记录；
- Provider、Admission 与 Manager 通过窄端口隔离，后续可以替换为对象存储和企业安全扫描实现。
- `version.content-reference.v1`：在 Ledger 写入前持久化 `Prepared` 保护，在精确 VersionRef 返回后幂等转为 `Confirmed`；上传 Lease 过期不会删除仍受版本保护的 CAS 对象；
- Prepared 保护有 tenant 数量配额和最长保护时间，重试会续展；Confirmed 保护不随 Head 移动或临时 Lease 回收。
- 可选 `version-content-upload` HTTPS 数据面：复用平台 `EndpointLease + ticket-redirect`，接受浏览器经同站 BFF 取得的一次性 Ticket，并把请求体直接流入 Manager；
- Ticket 精确绑定 tenant、用户、Exposure、实例、`PUT /v1/uploads/{uploadId}`、预期 SHA-256 和 30 秒有效期，原子消费一次，拒绝额外 query、明文 HTTP 与重放；
- 数据面接收成功后仍需调用 Workspace `completeContentUpload`，不会绕过摘要、大小和 Admission 准入。

## 当前边界

P2.4d1 已把真实字节流接到浏览器：Node Portal Kernel 复用通用 `/api/d/{routeKey}/ticket` BFF，Content Staging 提供受 EndpointLease 管理的 HTTPS 流式入口。它不是普通 JSON API，也没有 `writeChunk`；Node 不代理文件内容。

P2.4d2 已提供携带可信用户委托的 Backend/Desktop `private-direct` Go SDK 和独立 mTLS 入口。无用户委托的后台 workload grant、Node/Python SDK、对象存储 Provider、恶意内容/DLP 扫描和完整跨节点故障矩阵仍未实现；它们不能回退为静态共享 token。

内置 `IntegrityAdmission` 会再次顺序读取暂存内容，并配合 Manager 完成大小、SHA-256、mediaType 声明和 tenant/Lease 校验；它不是恶意软件扫描器。要求内容扫描的生产环境必须等待或配置 P2.4d 的 Admission Provider，不能把完整性校验误称为安全扫描。

本地 Provider 是 leader-owned 的单写实现。其 root 只允许规范绝对路径和属主私有的真实目录，不支持符号链接，也不会向调用方返回目录结构。

## 配置示例

```json
{
  "provider": {
    "protocol": "version.staging.storage.file.v1",
    "root": "/var/lib/vastplan/version-content-staging"
  },
  "limits": {
    "maxFileBytes": 1073741824,
    "maxTenantBytes": 10737418240,
    "maxTotalBytes": 107374182400,
    "maxActiveUploadsPerTenant": 64,
    "maxLeaseSeconds": 3600,
    "maxPreparedPerTenant": 256,
    "preparedProtectionSeconds": 86400,
    "terminalRetentionSeconds": 86400
  },
  "reclaimIntervalSeconds": 60,
  "dataPlane": {
    "listen": "127.0.0.1:9444",
    "endpoint": "https://content.internal:9444",
    "instanceId": "content-staging-1",
    "tlsIdentity": "spiffe://vastplan/content/content-staging-1",
    "allowedBrowserOrigins": ["https://portal.example.com"],
    "exposures": [
      { "tenantId": "tenant-a", "exposureId": "dpx_aaaaaaaaaaaaaaaaaaaa" }
    ],
    "private": {
      "listen": "127.0.0.1:9445",
      "endpoint": "https://content-private.internal:9445",
      "instanceId": "content-staging-private-1",
      "tlsIdentity": "spiffe://vastplan/content/content-staging-private-1",
      "clientIdentityPrefixes": ["spiffe://vastplan/backend/", "spiffe://vastplan/desktop/"]
    }
  }
}
```

协议允许的 1 TiB 只是硬上限，不是推荐配置。服务配置必须根据磁盘容量、扫描吞吐和并发基线设置更低限额。

`dataPlane` 可省略；省略后控制面和内部 Go 流端口仍可用，但不会登记浏览器上传 EndpointLease。启用时还必须：

1. 在 API Exposure 中发布引用本插件 `version-content-upload` 服务的 Data Plane Exposure，只批准实际 HTTPS origin、SPIFFE identity prefix、认证 Profile、所需权限和 `ticket-redirect`；
2. 通过 `VASTPLAN_CONTENT_UPLOAD_TLS_CERT` 与 `VASTPLAN_CONTENT_UPLOAD_TLS_KEY` 向受管进程提供证书和私钥路径；
3. 保证 `endpoint` 是客户端可达且与 `listen` 对应的无路径 HTTPS origin；在 `exposures` 中为每个启用浏览器上传的 tenant 绑定其已发布且唯一的 Exposure ID，EndpointLease 按 tenant 独立注册和续租；
4. 把实际 Portal origin 加入 `allowedBrowserOrigins`。只接受规范小写 HTTPS origin；本地开发仅允许 `localhost/127.0.0.1/[::1]` 使用 HTTP。未配置的 Origin 和额外预检 Header 均拒绝，禁止使用通配符 `*`。

启用 `private` 后，还需通过 `VASTPLAN_CONTENT_UPLOAD_PRIVATE_TLS_CERT`、`VASTPLAN_CONTENT_UPLOAD_PRIVATE_TLS_KEY` 和 `VASTPLAN_CONTENT_UPLOAD_PRIVATE_CLIENT_CA` 配置服务证书、私钥与客户端 CA。Private 监听强制 TLS 1.3 mTLS，客户端证书 URI SAN 必须命中显式 SPIFFE 前缀；服务端证书校验、mTLS 身份和一次性 Ticket 任一失败都会拒绝上传。

浏览器流程为：Workspace `beginContentUpload` → 同站 `POST /api/d/{routeKey}/ticket`（body 指定 `PUT`、`/v1/uploads/{uploadId}`、`contentSha256`）→ `PUT {endpoint}/v1/uploads/{uploadId}?vp_ticket=...` → Workspace `completeContentUpload`。Ticket URL 不得写日志、缓存或持久状态。

## 开发验证

```bash
GOCACHE=/tmp/vastplan-gocache go test -race ./extensions/plugins/cn.vastplan.foundation.versioning.content-staging/...
```

架构边界见[版本环境与资源适配](../../../docs/dev/architecture/版本环境与资源适配.md)和 [ADR-0176](../../../docs/dev/decisions/ADR-0176-租约式Content-Staging与流式数据面.md)。
