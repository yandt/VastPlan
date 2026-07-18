# ADR-0075：Portal 管理绑定与多平台基线

- 状态：已接受
- 日期：2026-07-19
- 修订：ADR-0068 第 2、3 项中的全局 `/v1/platform/*` 路径与固定 `routingDomain=platform` 假设

## 背景

同一企业既可能用一个运营 Portal 管理多台服务，也可能把运营、研发和审计拆成两个或三个 Portal。不同 Portal 可以共享同一个逻辑服务，也可以针对同一 capability 指向不同区域、环境或服务实例。若 Edge 仍使用全局平台路径和固定路由，浏览器页面无法区分目标，Portal 也无法形成最小授权边界。

## 决策

1. 平台输入采用不可变 `PortalPlatformCatalog`，其中包含多个 `PlatformProfile` 和按 `(tenantId, portalId)` 唯一的 `PortalBinding`。每个绑定以精确 `id + revision + digest` 引用一份 Profile。
2. `PortalBinding.services[]` 为 Portal 分配平台拥有的 opaque `serviceId`，并由平台管理员固定 `logicalService + routingDomain + capability grants`。同一逻辑服务可以显式绑定到多个 Portal；不会因 ID 相同而隐式共享。
3. grant 精确区分 capability 的 `read` 与 `write` operation，不接受通配符。浏览器只提交 URL 中的 `portalId + serviceId + 强类型资源路径`，不能提交 logical service、routing domain、capability、operation 或 tenant。
4. 强类型 BFF 路径为 `/v1/portals/{portalId}/platform/services/{serviceId}/...`。Edge 先从当前租户与 Portal 的活动 revision 解析并核对绑定摘要、访问受众和 operation grant，再把平台锁定的精确目标写入 `CallTarget`；远端能力宿主继续执行最终权限策略。
5. 一个 Portal 可绑定多个不同服务，也可绑定多个提供同一 capability 的服务；功能插件按绑定动态生成服务级页面。多个 Portal 可分别拥有不同基线和管理范围，也可显式重叠。
6. 同一逻辑服务的多个副本属于 Backend 调度与寻址层的集群实现，对 Portal 保持一个服务绑定；需要人为区分的环境、区域或独立集群使用不同 `serviceId`/target。
7. Catalog 在 Portal Edge/Composer 进程生命周期内不可热改。平台升级使用候选实例预检与进程切换；Application Composition 仍只能编辑功能插件和页面组合。

## 否决方案

- 单全局 Profile：无法让不同 Portal 使用不同 UI 基线和管理范围。
- 浏览器直接传 capability 或 logical service：会把新增后端能力意外暴露为通用代理。
- 以用户角色替代 Portal 绑定：角色只能说明“谁”，不能说明“此 Portal 被允许管理哪一个服务”。
- 为每个服务复制一套 Portal：简单场景可行，但不能满足单门户跨服务运营，也会重复发布与治理。

## 结果

Portal 的 UI 组合、访问受众、服务目标和操作权限形成同一份摘要锁；单门户和多门户模式使用同一契约。代价是平台基线变更必须重新生成受影响 Portal revision，且服务配置控制面需要维护 Catalog 与 Backend 服务拓扑的一致性。
