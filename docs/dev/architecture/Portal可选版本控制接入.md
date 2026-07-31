# P2.3 Portal 可选版本控制接入

本文是 P2.3 的实施真相源。版本控制决策见 [ADR-0174](../decisions/ADR-0174-Portal可选版本控制与发布快照分离.md)，Workspace 与 Ledger 边界分别见[版本环境与资源适配](版本环境与资源适配.md)和[通用版本账本](通用版本账本.md)。

## 目标

Portal 在没有任何版本控制配置时像普通目录：管理员可以修改、校验、审批、发布和上线当前配置，但没有 commit、VersionRef、Head、版本历史、历史 diff 或恢复版本。只有可信部署配置显式接入 Version Workspace 后，Portal 才获得版本管理。

P2.3 不把发布安全降级为“直接覆盖线上配置”。无论是否启用版本控制，待审批内容都必须冻结，Release 都必须精确、不可变、可物化和可审计。

## 概念拆分

```text
Portal
├── WorkingCopy                  可变的当前工作配置
│   ├── configuration
│   ├── digest
│   └── revision                仅为 CAS，不是版本号
├── PendingPublication?         当前冻结审批候选
├── CurrentRelease?             当前线上运行事实
├── ReleaseHistory[]            部署审计与回滚来源
└── VersionControlBinding?      缺失即没有版本管理
    ├── environmentId
    ├── resourceType
    └── latestVersionRef?
```

`Publication` 替代当前 `PortalVersion` 的审批职责：

- `PendingApproval → Approved → Published`；
- 保存提交人、审批人、发布人、冻结 digest 和来源；
- 无版本控制时来源是内联的规范化 PortalConfiguration；
- 启用 Workspace 时来源是精确 VersionRef，并保留当前候选热投影；
- Publication 不能被当作自由分支、checkpoint 或完整工作历史。

同一 Portal 最多一个未结束 Publication。PendingApproval 或 Approved 期间 WorkingCopy 暂停修改，Published 后才进入下一轮编辑，避免审批快照与可见工作内容产生两套“当前值”。

`PortalRelease` 继续承担上线职责。它只接受 Published Publication，重新执行当前制品信任、撤销、物化、路由冲突和 CAS 校验，并保存 PortalSpec、制品引用和前一 Release。历史回滚创建新的 Release，不修改旧记录。

## 配置与能力发现

未出现 `versionControl` 配置即为关闭，不需要写出一个伪 `provider: local`：

```yaml
portalComposer: {}
```

启用时只配置中立环境，不暴露插件 ID、Ledger Provider 或存储位置：

```yaml
portalComposer:
  versionControl:
    environmentId: platform-production
    resourceType: portal.configuration
```

P2.3 首版按 Portal Composer 服务配置解析一个受信默认绑定，并在 Portal 创建时固定是否启用；浏览器不能切换。在线 attach/detach、不同 Portal 的覆盖规则和历史迁移不进入 P2.3，避免在没有生产数据前建立迁移状态机。

Portal Composer 清单对 `foundation.versioning.workspace` 使用 `lazy + degrade`：

- 未配置绑定：不发出 Workspace 调用，Portal readiness 正常，UI 不显示版本入口；
- 已配置且能力健康：返回 `history/read/diff/restore` 能力；
- 已配置但能力不可用：当前工作副本、已发布 Publication、Release 列表和线上 Runtime 可读，新的版本提交及冷历史读取返回稳定 `version_control_unavailable`；
- 不运行后台轮询。能力目录事件更新可用性，实际操作仍以调用结果为准。

`lazy` 表示缺少 Workspace 不影响 Portal Composer 的基础 readiness，也不阻断 Seed Recovery Capsule 的 PlatformReady；只有启用版本控制后的真实 Workspace 操作才解析该能力并 fail-closed。

启用时 Portal 先调用 `describeResource` 解析该 Environment/Resource 的语义能力，再只向浏览器投影 `history/read/diff/restore` 等产品能力。Adapter ID、配置和存储路由不进入浏览器；若 Adapter 不支持 diff，版本历史和恢复仍可用，但 UI 不显示 compare。

## 两条写入路径

### 未启用版本控制

1. `saveWorkingCopy` 规范化并以 working revision CAS 覆盖当前配置。
2. `submitPublication` 在同一个 Portal 聚合 CAS 内复制并冻结当前配置、digest 和提交人，工作副本在候选结束前不可继续修改。
3. `approvePublication` 执行异人审批，只改变领域状态。
4. `publishPublication` 再次复核冻结内容与 Catalog，只冻结发布资格，不改变线上 Portal。
5. `releasePortal` 物化 Published Publication 并以当前 Release CAS 上线。

这条路径没有 VersionRef、Head 和版本历史。ReleaseHistory 只服务运行恢复与部署审计；管理界面不得把它标成“版本历史”。

### 启用 Workspace

1. `saveWorkingCopy` 仍只更新 Portal 热工作副本，不因每次输入产生 Ledger 版本。
2. `submitPublication` 以 Portal 保存的 latestVersionRef 作为 BaseRef 打开 Workspace，写入完整 PortalConfiguration。
3. Portal 使用持久 operation ID 提交 detached VersionRef，不设置 TargetHead。
4. Workspace 返回 VersionRef 后，Portal 以聚合 CAS 同时保存 Publication、VersionRef、latestVersionRef 和轻量历史元数据。
5. CAS 失败最多留下不可达 Ledger 版本；重试使用同一 operation ID，不能产生第二个逻辑版本。
6. 审批、发布和 Release 与无版本模式复用同一领域状态机。

P2.3 不移动通用 Head。Portal 聚合保存的 VersionRef 列表是可见历史真相源，避免 Head 在领域 CAS 前变化。未来若确有 GitOps/导出需求，再通过 outbox 和独立窄操作镜像 Head。

## Workspace P2.2.1 前置调整

现有 `open/readSnapshot/writeSnapshot/changes/commit/discard/renew` 结构保持不变，但完整 P2.3 需要三个收窄调整：

1. `CommitRequest` 增加必填 `operationId`。它由可信领域插件生成并在外部调用前持久化；Ledger 继续以可信 tenant、Workspace 解析的 stream 和稳定 operationId 派生逻辑版本身份，不再只依赖临时 sessionId/revision。Workspace Leader 丢失 Session 后，Portal 可用同一 operationId 重开并安全重试。
2. 增加 `readCommitted`：输入 Environment ID、提交时固定的 Environment digest、Resource 与精确 VersionRef，经原始绑定 Adapter 规范化后返回 Snapshot。Portal 不解析 Ledger 内容编码，也不直调 Provider；原始 digest 未加载时失败关闭。
3. 增加 `compareCommitted`：输入同一环境摘要、同一资源的两个 VersionRef，读取后交给 Resource Adapter diff，返回确定性路径与统计。

历史列表不进入 Workspace。Portal 只列出自己聚合 CAS 已确认的轻量 VersionRef 元数据，防止 Ledger 中不可达版本或其他领域记录绕过 Portal 权限、审批和可见性规则。

恢复历史版本使用现有能力组合：`readCommitted` 按历史记录固定的 Environment digest 读取历史快照，再经过当前领域规则校验后覆盖 WorkingCopy；它只是一次新的工作副本变更，后续提交会产生新 VersionRef，不能修改旧版本。

## Workspace P2.2.2 能力协商前置调整

按 [ADR-0175](../decisions/ADR-0175-统一版本生命周期与Resource-Adapter能力协商.md) 增加：

1. `describeResource`：返回受信绑定解析后的 contentKind、允许模式、大小上限和 normalize/diff/materialize/merge 能力位。
2. `ChangesResult/CompareCommittedResult` 将摘要变化与详细 diff 分开：`dirty` 永远由 digest 决定；`diffAvailable=false` 时路径为空、统计为零，不能把变化伪装成 clean。
3. Adapter SPI 强制 `describe + normalize/validate`，其他操作按 Descriptor 协商；不支持的操作返回稳定 `operation_unsupported`。

P2.3 的 JSON Adapter 支持详细 diff，但 Portal 端口不能硬编码这一事实，必须消费 Workspace 返回的能力，确保以后 Text、Blob、Files 或领域 Adapter 接入时不分叉 API。

## 一致性与恢复

| 故障点 | 结果 | 恢复规则 |
|---|---|---|
| Workspace commit 前失败 | Portal 聚合不变 | 使用原 WorkingCopy 重试 |
| Ledger 写成功、响应丢失 | 可能存在不可达版本 | 同 operationId 重开并幂等取得同一 VersionRef |
| VersionRef 成功、Portal CAS 失败 | 版本暂不可达 | 同 operationId 重试；不得提前移动 Head |
| Portal CAS 成功、Workspace 随后故障 | Publication 和 VersionRef 已是领域事实 | 当前候选/Release 读热投影；冷历史暂不可用 |
| 未配置 Workspace | 不调用版本服务 | Portal 正常运行，不标记 Degraded |
| 已配置后故障 | 版本功能不可用 | 不得切到无版本模式；线上 Release 继续运行 |

WorkingCopy、当前 Publication 和 Current Release 必须保留完整有界热投影。普通 Portal 列表、编辑当前工作副本、当前运行描述和线上模块交付不能 hydrate 全部 Ledger 历史。

## API 与 UI 目标

P2.3 是开发期破坏性重构，Portal Composer 提升能力主版本和 Shared State format，不保留旧 `/versions` 模型：

- `GET /v1/portals`：Portal、WorkingCopy 摘要、当前 Publication/Release 和版本能力；
- `PUT /v1/portals/{portalId}/working-copy`：working revision CAS 保存；
- `POST /v1/portals/{portalId}/publications`：提交并冻结候选；
- `POST .../approve|publish`：推进 Publication；
- `POST /v1/portals/{portalId}/releases` 与 rollback：保持上线子流程；
- `GET /v1/portals/{portalId}/history`、`.../compare`、`.../restore`：只在版本控制启用时开放。

`portalGovernance` 返回语义能力，不暴露 Workspace 插件 ID：

```json
{
  "versionControl": {
    "enabled": true,
    "availability": "available",
    "capabilities": ["history", "read", "diff", "restore"]
  }
}
```

Workbench 在 `enabled=false` 时完全隐藏 commit/history/diff/restore；保留保存、提交审批、发布、上线和 ReleaseHistory。`enabled=true` 但 unavailable 时显示明确状态，不把操作静默改成普通保存。

## P2.3d 故障验证矩阵

P2.3d 使用确定性故障注入，不引入依赖计时的长时间混沌测试。进程级混沌和压力验证留到真实插件负载具备后执行。

| 场景 | 必须保持的性质 | 自动化证据 |
|---|---|---|
| 未配置 Workspace | 不调用版本端口，Publication 使用 inline 冻结源 | `TestPortalNoVersionLifecycleUsesWorkingCopyPublicationAndRelease` |
| Adapter 能力不同 | 只投影受支持的 read/diff/restore；history 仍来自 Portal 聚合 | `TestPortalVersionControlProjectsOnlySupportedCapabilities`、Workbench 能力动作测试 |
| 已绑定但软依赖缺失 | 明确 unavailable，保留 WorkingCopy 热投影，提交失败关闭且不退回 inline | `TestPortalVersionControlMissingSoftDependencyFailsOnlyBoundPortal` |
| Leader 重启、Workspace 已提交但响应丢失 | 持久 operationId 跨重启复用，同一逻辑提交只产生一个 VersionRef | `TestPortalVersionSubmitRecoversAfterLeaderRestartAndLostResponse`、Workspace `TestManagerOperationIdentitySurvivesSessionLossAndCommittedReads` |
| VersionRef 成功后聚合冲突 | 未确认版本不进入 Portal 历史；重试幂等确认原版本 | `TestPortalVersionAggregateConflictKeepsExternalCommitUnreachableUntilRetry` |
| 聚合提交成功但调用方未收到响应 | 相同请求直接返回同一 Publication，不再次调用 Workspace | `TestPortalVersionSubmitRecoversAfterLeaderRestartAndLostResponse` |
| 冷历史读取失败 | 当前 Published Publication、Release 和本地轻量历史仍可读 | `TestPortalReleaseAndHotProjectionSurviveColdVersionControlFailure` |
| Workspace/Ledger 离线时上线 | Release 只消费 Published Publication 热快照，不 hydrate Ledger | `TestPortalReleaseAndHotProjectionSurviveColdVersionControlFailure` |

能力可用性由实时 `describeResource` 的成功与能力位共同决定。`history` 是 Portal 聚合自身已确认的轻量索引；`read`、`diff` 和 `restore` 必须逐项协商，其中 restore 还要求 read。已配置但不可用时，UI 隐藏版本动作并显示 unavailable，不影响 ReleaseHistory 和当前运行交付。

## 实施分解

1. **P2.2.1（已完成）**：补 operationId、readCommitted、compareCommitted 的契约、SDK、Manager、Environment 多修订精确解析与故障测试。
2. **P2.2.2（已完成）**：补 describeResource、可选 diff 结果、Adapter 能力校验和不支持操作的稳定错误。
3. **P2.3a（已完成）**：Portal 聚合拆为 WorkingCopy/Publication/Release/版本控制语义状态，完成无版本路径、WorkingCopy revision CAS、Publication 冻结摘要和 v3 Shared State；旧 v2 operations 暂作同状态投影。
4. **P2.3b（已完成）**：实现中立 `PortalVersionControl` 端口和 Workspace Adapter，接通 detached commit、历史读取、比较与恢复。提交前先在 Portal Shared State 持久化 operation ID；聚合 CAS 只确认 Workspace 返回的精确 VersionRef。普通管理读取只对已绑定 Portal 按请求执行能力发现，不运行后台轮询。
5. **P2.3c（已完成）**：BFF、TypeScript SDK 与 Workbench 已切换到 WorkingCopy/Publication/Release 和可选 history/read/compare/restore；删除 `/versions`、旧 PortalVersion 用户操作与 `Portal.versions` 兼容状态。Portal Composer 提升到 4.0.0。
6. **P2.3d（已完成）**：已覆盖两种模式、能力差异、软依赖缺失、Leader 重启、响应丢失、聚合 CAS 冲突、历史冷读失败和 Release 不依赖 Ledger；采用确定性故障注入，未引入 soak 或依赖时序的混沌测试。
7. **P2.3e（已完成）**：增加真实进程纵向 E2E，同时启动 Portal Composer、Version Workspace、Version Ledger 与 Node Portal Kernel，经浏览器 BFF 验证两次提交、精确 VersionRef、历史冷读、差异比较、Publication 上线与历史恢复；不再只依赖 fake 版本端口证明 Portal 接线正确。

P2.3 不实施手工 checkpoint、branch、merge、在线 attach/detach、历史迁移、Head 镜像、生产 Session 持久化或 P2.4 的二进制 Content Staging。
