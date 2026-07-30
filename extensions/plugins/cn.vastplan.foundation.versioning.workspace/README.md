# Version Workspace

插件 ID：`cn.vastplan.foundation.versioning.workspace`

该基础插件在 Version Ledger 之上提供受信版本环境和有 Lease 的编辑会话。业务插件只选择 Environment、Resource 和可选 Head，不得选择 Ledger Provider、目录、数据库连接或凭证。

## P2.2 能力

- `version.workspace.v1`：open/status/readSnapshot/writeSnapshot/changes/commit/discard/renew；
- `version.resource.json.v1`：JSON object 规范化、摘要、确定性变化路径和疑似秘密明文拒绝；
- Session revision CAS、tenant + actor 所有权、Lease、租户活动 Session 配额；
- 提交先以 Session 派生幂等键写不可变版本，再创建或 CAS 移动 Head；
- Head 写响应丢失时执行 read-after-error，区分已成功、真实冲突和暂时不可用；
- Manager 每个服务宿主共享，当前 Session 仅保存在 leader 内存中。

当前不包含 Overlay、Git、生产级 Session 恢复或 Portal 迁移。Leader 重启后未提交 Session 会丢失，因此调用方必须把 Workspace 当成临时编辑区，不能把它当作领域事实存储。生产级跨实例 Session 持久化要等真实故障恢复需求出现后再设计，避免提前建立第二套版本真相源。

## 配置示例

```json
{
  "environments": [
    {
      "protocol": "version.resource.v1",
      "id": "platform-development",
      "revision": 1,
      "bindings": [
        {
          "resourceType": "portal.configuration",
          "namespace": "portal.configuration",
          "adapter": "version.resource.json.v1",
          "allowedModes": ["snapshot"],
          "defaultMode": "snapshot",
          "projectionPolicy": "domain-hot"
        }
      ],
      "limits": {
        "maxSessionsPerTenant": 64,
        "maxLeaseSeconds": 3600,
        "maxSnapshotBytes": 1048576,
        "maxOverlayBytes": 1048576
      }
    }
  ]
}
```

## 开发验证

```bash
go test -race ./extensions/plugins/cn.vastplan.foundation.versioning.workspace/versionworkspace
go test ./extensions/plugins/cn.vastplan.foundation.versioning.workspace/backend
```

架构边界见[版本环境与资源适配](../../../docs/dev/architecture/版本环境与资源适配.md)和 [ADR-0173](../../../docs/dev/decisions/ADR-0173-版本环境与资源适配.md)。
