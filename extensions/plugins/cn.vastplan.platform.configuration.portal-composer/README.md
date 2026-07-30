# Portal Composer Plugin

`cn.vastplan.platform.configuration.portal-composer` 是 Portal 在线组合与发布治理插件。管理中心只注册一个 `/settings/portals` Workbench 页面，一行代表一个 Portal。

每个 `PortalVersion` 保存完整配置：平台运行栈、Renderer、Shell、Workbench、路由、受众、品牌、应用插件、非敏感配置及管理服务授权。原 Platform Profile、Application 和 Binding 不再拥有独立在线草稿或菜单，避免管理员手工拼装多份 revision。

PortalVersion 执行 `Draft → PendingApproval → Approved → Published`。Published 只冻结版本，不代表线上生效。`PortalRelease` 引用一个精确 Published PortalVersion，重新执行可信 Catalog 校验、制品物化、引用保护、路由冲突检查和 CAS 后才改变线上 Portal。回滚使用历史 Release 对应的 PortalVersion 创建新 Release，不修改历史。

静态 Platform Catalog 仅为创建首个 PortalVersion 提供种子模板。它不会被灌入在线 Profile/Binding 治理状态。内核 Recovery Baseline 独立于 Portal 配置，错误 PortalVersion 不能覆盖最小安全启动和恢复入口。

Frontend Test Release 也形成完整候选 PortalVersion：获得授权的应用或平台插件槽位被替换后，候选整体校验、发布和上线，不产生对外可见的独立测试 Profile/Binding。

该插件从可信 `CallContext` 取得 tenant 与 Principal，通过 `kernel.portal.catalog.*` 窄能力校验和物化制品，不接触仓库凭据或验签密钥。全部用户 operation 与权限守卫由插件 Manifest 机械投影。

同一 active-active 逻辑服务还提供独立 `platform.portal-preference` 能力。偏好按 tenant、subject、Portal 与 UI Contract scope 保存，不属于 PortalVersion 发布状态。

```bash
pnpm --filter @vastplan/portal-composer typecheck
pnpm --filter @vastplan/portal-composer test
```

完整边界见《[前端门户内核](../../../docs/dev/architecture/前端门户内核.md)》和 [ADR-0171](../../../docs/dev/decisions/ADR-0171-Portal单聚合版本与上线子流程.md)。
