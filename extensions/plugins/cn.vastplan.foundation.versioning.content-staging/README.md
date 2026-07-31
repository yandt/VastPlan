# Version Content Staging

插件 ID：`cn.vastplan.foundation.versioning.content-staging`

该基础插件在 Version Workspace 与真实文件字节之间提供受信暂存边界。它管理短时 Upload Lease、流式摘要与大小校验、tenant 配额、本地内容寻址对象和过期回收；它不把文件字节、物理路径、存储 Provider、endpoint、凭据、token 或上传 URL 放进 JSON Capability Bus。

## P2.4b 能力

- `version.staging.v1` 控制面：`beginUpload/uploadStatus/renewUpload/completeUpload/abortUpload`，`beginUpload` 强制使用可信 `CallContext` 的 idempotency key 防止响应丢失后重复预留；
- 仅接受 `cn.vastplan.foundation.versioning.workspace` 的受信控制调用，并从 `CallContext` 取得 tenant 与 subject；
- `Manager.Stream(ctx, scope, uploadID, io.Reader)` 是独立 Go 数据面端口，按 128 KiB 缓冲流式读取，不在内存中聚合整个文件；
- `Pending/Uploading/Verifying/Ready/Rejected/Aborted/Expired` 状态机，控制转换使用 revision CAS，流式进度不增加 revision；
- 单文件、tenant 总量、服务总量和 tenant 活动上传数四级配额；
- 本地 File Provider 使用 tenant 哈希分区、权限 `0700/0600`、严格状态 JSON、SHA-256 CAS 对象、硬链接原子发布和目录 fsync；
- 启动恢复会复核全部 Ready 对象，缺失或摘要不符时失败关闭；
- 后台周期回收和启动回收会清理过期 Lease、无引用临时对象及超过保留期的终态记录；
- Provider、Admission 与 Manager 通过窄端口隔离，后续可以替换为对象存储和企业安全扫描实现。

## 当前边界

P2.4c1 已由 Workspace 接入 Lease 控制面和 Files Manifest Ready 校验，但仍未把真实字节流接到浏览器、Runner 或 Backend Host。当前没有 `writeChunk` 或临时 HTTP 上传接口。P2.4c2 将补齐 durable version-reference outbox 并开放 Files commit；P2.4d 再提供同站 BFF、Host streaming SDK、对象存储 Provider 与恶意内容/DLP 扫描。

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
    "terminalRetentionSeconds": 86400
  },
  "reclaimIntervalSeconds": 60
}
```

协议允许的 1 TiB 只是硬上限，不是推荐配置。服务配置必须根据磁盘容量、扫描吞吐和并发基线设置更低限额。

## 开发验证

```bash
GOCACHE=/tmp/vastplan-gocache go test -race ./extensions/plugins/cn.vastplan.foundation.versioning.content-staging/...
```

架构边界见[版本环境与资源适配](../../../docs/dev/architecture/版本环境与资源适配.md)和 [ADR-0176](../../../docs/dev/decisions/ADR-0176-租约式Content-Staging与流式数据面.md)。
