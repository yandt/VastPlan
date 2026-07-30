# Portal Composer Plugin

`cn.vastplan.platform.configuration.portal-composer` 是 Portal 在线组合与发布治理插件。管理中心只注册一个 `/settings/portals` Workbench 页面，一行代表一个 Portal。

每个 Portal 由一个可变 `WorkingCopy`、至多一个待结束 `Publication`、最近 Published Publication 和 `PortalRelease` 历史组成。完整配置包括平台运行栈、Renderer、Shell、Workbench、路由、受众、品牌、应用插件、非敏感配置及管理服务授权。

WorkingCopy 保存使用独立 revision CAS，不产生业务版本；提交时冻结规范配置和 SHA-256 digest，形成 `PendingApproval → Approved → Published` Publication。审批期间没有可编辑 WorkingCopy。`PortalRelease` 引用精确 Published Publication，重新执行可信 Catalog 校验、制品物化、引用保护、路由冲突检查和 CAS 后才改变线上 Portal。

版本控制是可选能力。未配置 `platform.portal-composer.versionControl` 时，`versionControl` 明确返回 `enabled=false / disabled`，不发出 Workspace 调用，也不创建 VersionRef、Head 或通用历史。配置可信 `environmentId + resourceType` 后，Portal 创建时固定绑定；提交 Publication 使用持久 operation ID 创建 detached VersionRef，Portal 聚合只公开自身 CAS 已确认的轻量历史。旧 PortalVersion operations 仅作为 P2.3c 前的同状态兼容投影，不保存第二份数据。

```json
{
  "platform.portal-composer.versionControl": {
    "environmentId": "platform-production",
    "resourceType": "portal.configuration"
  }
}
```

历史读取和比较通过中立 `PortalVersionControl` 端口完成。恢复历史只覆盖现有 WorkingCopy，并继续执行当前配置规范化、Catalog 校验与 working revision CAS；它不会修改旧版本。Workspace 不可用时，当前配置、Publication 热投影、Release 和运行交付仍可读，但提交与冷历史操作失败关闭。

静态 Platform Catalog 仅为创建首个 PortalVersion 提供种子模板。它不会被灌入在线 Profile/Binding 治理状态。内核 Recovery Baseline 独立于 Portal 配置，错误 PortalVersion 不能覆盖最小安全启动和恢复入口。

Frontend Test Release 也形成完整候选 PortalVersion：获得授权的应用或平台插件槽位被替换后，候选整体校验、发布和上线，不产生对外可见的独立测试 Profile/Binding。

该插件从可信 `CallContext` 取得 tenant 与 Principal，通过 `kernel.portal.catalog.*` 窄能力校验和物化制品，不接触仓库凭据或验签密钥。全部用户 operation 与权限守卫由插件 Manifest 机械投影。

同一 active-active 逻辑服务还提供独立 `platform.portal-preference` 能力。偏好按 tenant、subject、Portal 与 UI Contract scope 保存，不属于 PortalVersion 发布状态。

```bash
pnpm --filter @vastplan/portal-composer typecheck
pnpm --filter @vastplan/portal-composer test
```

完整边界见[Portal 可选版本控制接入](../../../docs/dev/architecture/Portal可选版本控制接入.md)和 [ADR-0174](../../../docs/dev/decisions/ADR-0174-Portal可选版本控制与发布快照分离.md)。
