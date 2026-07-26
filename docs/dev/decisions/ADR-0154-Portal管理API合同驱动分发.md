# ADR-0154 Portal 管理 API 合同驱动分发

- 状态：已采纳，已实施首个迁移试点
- 日期：2026-07-26
- 关联：[ADR-0068](ADR-0068-分布式平台管理中心与强类型BFF.md)、[ADR-0075](ADR-0075-Portal管理绑定与多平台基线.md)、[ADR-0107](ADR-0107-插件权限目录与系统管理授权治理.md)、[ADR-0110](ADR-0110-治理式API-Exposure与独立数据面.md)

## 背景

Node Portal Kernel 过去为每个管理领域维护一组 TypeScript 路由类，并在内核中重复登记 `capability + operation` 字典。新增一个提供管理页面的插件必须修改 Portal Kernel，即使插件已经在已验签 Manifest 中声明了完整 `apiContracts`。这使内核逐步认识业务领域，扩大回归面，也让前端插件热升级仍受宿主版本约束。

ADR-0068 正确否决了由浏览器自由提交 capability、operation 或 logical service 的通用代理。本决策保留该安全结论，只替换“每个业务 HTTP 映射必须由内核手写”的实现方式。

## 决策

### 1. PortalBinding 只发布不透明 API 选择器

每个 `ManagedService` 可声明有界 `apis` 目录。目录项只包含 Portal 内不透明 `apiId`，以及 API Contract 的精确 `contractId + contractVersion + contractDigest`。它不包含插件 ID、capability、operation、logical service 或 routing domain。

浏览器只能请求：

`/v1/portals/{portalId}/platform/services/{serviceId}/api/{apiId}{contractPath}`

其中前三个选择器必须来自当前已解析 PortalBinding，后续路径与方法必须精确匹配被锁定 Contract。浏览器不能通过 query、header 或 body 覆盖调用目标。

### 2. 可信宿主从已验证制品生成 Contract Catalog

Go 可信宿主从已验证 Manifest 生成私有 `API Contract Catalog`。Node 以 `O_NOFOLLOW` 打开普通只读文件，拒绝组/其他用户可写文件、超限文件和 generation 回退，并保留最近一次有效快照。Binding 中的精确摘要必须与 Catalog 命中；缺失、漂移或 Schema 无效均 fail-closed。

Node 只从 Contract 路由取得 capability、operation、请求/响应 Schema、成功码和错误映射。请求体、query 与响应均受大小上限和 JSON Schema 严格验证，写操作继续要求会话与 CSRF。

### 3. 授权仍由三层共同决定

通用分发不等于通用授权。一次调用必须同时满足：

1. 当前 PortalActivation 的受众和精确 Management Binding；
2. Binding 对 Contract 目标 operation 的 read/write grant；
3. Backend PEP 对可信 CallContext、permission、scope 和 workload policy 的最终裁决。

Node 不重新维护业务 operation→role 字典。页面动作的 `requiredPermissions` 只用于体验预检，不能代替 Backend PEP。

### 4. 渐进迁移而非一次删除全部类型路由

`@vastplan/platform-admin` 提供通用 `ManagementAPIClient`，但领域前端必须在插件内部包一层窄类型 Port，页面不能直接散布任意相对路径。API Exposure 管理 API 是首个试点；其 Data Plane 管理页面和其他领域暂时保留旧类型 BFF，待各自 Contract、错误语义和测试齐备后逐项迁移。

所有新管理插件默认使用合同驱动路径。只有会话、CSRF、认证回调、Portal 治理与大文件/流式数据等确实属于宿主协议的端点，才可新增内核固定路由。

## 备选方案

### 继续为每个插件手写 Node 路由

类型最直观，但新增插件持续修改内核，重复 capability 字典并阻碍独立升级，否决为默认方案。

### 暴露通用 capability/operation 代理

实现最少，却把可信寻址与授权目标交给浏览器，存在 confused deputy 和未来能力扩张风险，继续否决。

### 只按 Contract ID 查询最新版本

便于升级，但运行时会静默漂移，无法保证 Activation 可复现和回滚，否决；必须锁定版本与摘要。

## 影响

- 新管理插件只需发布受治理 API Contract、Backend capability 和前端窄 Port，无需修改 Portal Kernel 业务路由；
- Portal Kernel 仍拥有 HTTP 安全和协议分发，但不再拥有领域 operation 字典；
- Contract 变更必须提升插件 SemVer，并经制品发布、Catalog 生成、Binding 发布与 Activation 流程上线；
- 旧类型路由在迁移期间允许存在，但不得再作为新领域的默认模板；
- API Exposure `0.5.4` 成为首个合同驱动管理页面试点。
