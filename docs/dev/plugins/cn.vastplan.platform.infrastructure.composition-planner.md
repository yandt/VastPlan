# Application Composition Planner

插件 ID：`cn.vastplan.platform.infrastructure.composition-planner`

当前制品版本：`0.1.1`

该平台基础插件提供 `tool.package/platform.composition.plan`，把 Backend `ApplicationIntent` 编译为只读 `ApplicationComposition`、Artifact Lock、Configuration Plan、Provider Binding、Service DAG 与确定性 `planDigest`。它是无状态、可水平扩展的 `active-active + external-shared + cluster + queue` 服务，不进入 Seed Core 或仓库 LKG 子集。

## 输入与配置闭环

调用方只能是宿主认证的 `cn.vastplan.platform.infrastructure.deployment-manager`。请求包含用户 Intent、平台选定的 Platform Profile，以及可选的可信 `ConfigurationSnapshot`：

- Intent 只含根应用插件、Feature、非敏感配置和受限运维参数；
- Platform Profile 固定 service class、Baseline 与共享 Platform Provider；
- Configuration Snapshot 由配置 Provider 生成，只含不透明 `ManagedCredentialRef`，用户不能在 Intent 中手写引用；
- 没有必填配置或 CredentialRef 时返回 `NeedsConfiguration`；配置插件完成托管后重新规划，可收敛为 `Resolved`。

Planner 配置固定自身 channel、Backend 内核版本、目标平台、允许的 channel、publisher、插件前缀和开发插件策略。这些字段规范化后绑定到 `planner.configurationDigest`，避免同一插件版本在不同解析策略下产生无法解释的结果。

## 仓库边界

Planner 只调用仓库的两个只读操作：

- `resolve`：由仓库完成 SemVer、传递包依赖、生命周期和 Catalog revision 求解；Intent 与 Profile 根制品的精确 channel 同时进入锁，不能在同版本的 stable/testing 间漂移；
- `describePlanning`：按精确 ref 返回 SHA-256、publisher 与已验签 Manifest，不返回包体、对象路径、存储 Provider 或仓库凭证。

Feature 条件依赖按选择它的 service 隔离，不会泄漏到使用同一根插件的其他 service。Platform/Foundation 插件不能经应用包依赖偷偷进入组合，只能由 Platform Profile 的 Baseline 或共享服务提供。

## 运行时推导

同一 service 中所有非辅助贡献的签名运行策略必须一致；本地权限辅助插件可以按既有窄例外共置。跨服务边只来自 Manifest `runtime.requires` 或已启用 Feature 的 `runtimeRequires`。多个 Provider 无法由签名 `logicalService/routingDomain/version` 消除歧义时返回 `Invalid`，不让用户手工指定内部 Provider。

Planner 是建议生成者：不持有 NATS KV、Deployment CAS、SSH、Assignment、仓库签名密钥、信任根或凭证 material。Backend 内核、Controller 与 Node Agent 继续从完整 Manifest 独立复验和执行。

实现决策见 [ADR-0143](../decisions/ADR-0143-插件化应用意图规划与派生依赖.md)。
