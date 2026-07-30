# Version Ledger

插件 ID：`cn.vastplan.foundation.versioning.ledger`

该基础插件统一保存不可变 JSON 配置版本、父版本链和命名 Head。审批、发布、上线、回滚和业务权限仍由 Portal、Deployment 等领域插件负责，Ledger 不接管领域状态机。

## 当前能力

- `version.ledger.v1`：幂等写入、精确读取、父链分页、Head CAS。
- `version.storage.file.v1`：本地私有目录、单 writer、版本文件不可变、Head 原子替换。
- 内存 Provider：只用于契约测试，不允许从生产配置启用。
- namespace 精确路由：调用方不能在请求中指定 Provider。

File Provider 会把 tenant 和 stream 标识哈希后写入目录；配置的 root 必须是绝对路径和权限 0700 的专用真实目录，不能使用文件系统根目录，插件不会替调用者修改已有目录权限。版本写入使用临时文件、文件 `fsync`、create-only hard link 和目录 `fsync`。崩溃遗留的 `.version-*` 临时文件会被忽略，已提交版本的摘要、父链、文件名或 sequence 损坏会 fail-closed。

## 配置

```json
{
  "defaultProvider": "local-primary",
  "providers": [
    {
      "id": "local-primary",
      "protocol": "version.storage.file.v1",
      "root": "/var/lib/vastplan/version-ledger"
    }
  ],
  "routes": [
    { "namespace": "portal.configuration", "provider": "local-primary" }
  ]
}
```

P1 清单采用 leader / leader-owned 拓扑，明确不把 File Provider 宣称为集群共享存储。未来 Relational Provider 即使接入，也必须通过同一 Provider SPI 在数据库事务内分配 sequence、versionId 和 createdAt。

## 开发验证

```bash
go test -race ./extensions/plugins/cn.vastplan.foundation.versioning.ledger/versionledger
go test ./contracts/schemas/versioning/v1
```

架构与事务边界见 [通用版本账本](../../../docs/dev/architecture/通用版本账本.md) 和 [ADR-0172](../../../docs/dev/decisions/ADR-0172-通用版本账本与可插拔存储Provider.md)。
