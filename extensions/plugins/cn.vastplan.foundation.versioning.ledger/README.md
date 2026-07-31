# Version Ledger

插件 ID：`cn.vastplan.foundation.versioning.ledger`

该基础插件统一保存不可变 JSON 配置版本、父版本链和命名 Head。审批、发布、上线、回滚和业务权限仍由 Portal、Deployment 等领域插件负责，Ledger 不接管领域状态机。

## 当前能力

- `version.ledger.v1`：幂等写入、精确读取、双父 DAG、第一父历史分页、Head CAS、不可变 Tag、确定性 JSON Patch 和祖先查询。
- `version.identity.v1`：所有 Provider 使用相同的逻辑 versionId 派生规则和跨语言 golden vectors。
- `version.storage.file.v1`：本地私有目录、单 writer、版本文件不可变、Head 原子替换。
- 内存 Provider：只用于契约测试，不允许从生产配置启用。
- namespace 精确路由：调用方不能在请求中指定 Provider。

File Provider 会把 tenant 和 stream 标识哈希后写入目录；配置的 root 必须是绝对路径和权限 0700 的专用真实目录，不能使用文件系统根目录，插件不会替调用者修改已有目录权限。版本写入使用临时文件、文件 `fsync`、create-only hard link 和目录 `fsync`。Head 删除会原子写入修订墓碑，重建时延续 revision 以避免 ABA。崩溃遗留的 `.version-*` 临时文件会被忽略，已提交版本的摘要、父 DAG、引用、文件名或 sequence 损坏会 fail-closed。

当前 Git 式能力是存储中立的逻辑语义，不等同于 Git Provider：支持分支式 Head、发布 Tag、双父记录、diff 和祖先关系，但不提供工作区、三方合并、冲突标记、rebase、remote 或 Git 对象协议。

`0.2.0` 将 File Provider 数据格式提升为 v2（`parent` 改为最多两个 `parents`，Head 增加删除墓碑）。项目尚处开发阶段，不读取 `0.1.x` 的 File Provider 数据；本地旧测试目录需要清空后重新生成，不能静默混用两种格式。

`0.2.1` 修复 VersionRecord 防御性复制时标签丢失的问题；标签现在会完整保留，并在 Ledger 返回结果与候选一致性校验中得到真实验证。该修复不改变 File Provider 数据格式。

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

P1.5 清单采用 leader / leader-owned 拓扑，明确不把 File Provider 宣称为集群共享存储。未来 Relational Provider 即使接入，也必须复核 Ledger 按 `version.identity.v1` 派生的 versionId，并通过同一 Provider SPI 在数据库事务内分配 sequence 和 createdAt。

## 开发验证

```bash
go test -race ./extensions/plugins/cn.vastplan.foundation.versioning.ledger/versionledger
go test ./contracts/schemas/versioning/v1
```

架构与事务边界见 [通用版本账本](../../../docs/dev/architecture/通用版本账本.md) 和 [ADR-0172](../../../docs/dev/decisions/ADR-0172-通用版本账本与可插拔存储Provider.md)。
