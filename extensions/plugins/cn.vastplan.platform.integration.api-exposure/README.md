# API Exposure

`cn.vastplan.platform.integration.api-exposure` 是平台级 API 暴露治理插件。它把已验签插件清单中的 `apiContracts` 和 `dataPlaneServices` 转换为经过审批的公开入口，不允许插件自行占用公网路径。

## 提供能力

- HTTP API Exposure 的草稿、异人审批、发布、替换与退役；
- 平台生成并永久墓碑化的 96-bit 随机 Route Key；
- 向 Node Portal Gateway 原子发布自包含 Catalog；
- 独立数据面 Exposure、最长 5 分钟的 Endpoint Lease；
- 绑定租户、主体、资源和实例的 30 秒一次性 Ticket；
- Workbench 管理页面和固定 BFF 路由。

公开普通 API 使用 `/api/r/{routeKey}/v{major}/...`，数据面 Ticket 使用 `/api/d/{routeKey}/ticket`。两类地址都不包含插件 ID、capability、服务名、节点或内部 endpoint。

## 配置

插件以首方 Go 独立可信进程运行，启动快照必须提供：

| 字段 | 说明 |
|---|---|
| `stateFile` | 私有治理状态文件 |
| `gatewayCatalogFile` | Node Portal Gateway 只读的原子 Catalog 文件 |
| `contractCatalogFile` | 可信宿主从已验签制品生成的私有 Catalog 文件；不接受浏览器拼装 |

状态和 Gateway Catalog 必须位于受控私有目录。插件启动时完成状态恢复和 Catalog 重放，配置错误会使进程 fail-closed。

## 开发语言与运行形态

控制面选择 Go，是因为生命周期状态、原子文件发布、并发 Lease/Ticket 和既有 Backend SDK 的实现成本最低；HTTP Gateway 保持在现有 Node Portal Kernel，以复用认证、BFF 与 JSON Schema 生态。业务插件的实现语言不受限制：Go、Node.js、Python 或其他 Runtime 只需声明相同的签名清单契约，并通过稳定 `tool.package`/Endpoint Lease 协议接入。

权威设计见 [`docs/dev/architecture/API暴露与数据面服务.md`](../../../../docs/dev/architecture/API暴露与数据面服务.md)。
