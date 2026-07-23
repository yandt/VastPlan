# ADR-0118 独立配置资源与动态 Profile

- 状态：已采纳，首个纵向闭环已实施
- 日期：2026-07-23
- 关联：[ADR-0090](ADR-0090-插件配置与托管凭证闭环.md)、[ADR-0114](ADR-0114-一次性ConfigurationAuthority与委托凭证暂存.md)、[ADR-0117](ADR-0117-语言中立Service-Hot配置控制器.md)

## 背景

OIDC 与 Webhook Delivery 都需要在运行时维护 0—N 个 Profile。每个 Profile 有独立的非敏感值、托管秘密、引用者、修订、审计、删除和轮换生命周期。把秘密引用放在根配置的 `profiles.*` 动态路径中，会让签名清单无法固定凭证字段，Profile 改名后也无法稳定判断保留、替换和退休关系。

路径通配符方案改动较小，但会把 JSON Pointer 授权、数组/对象变形、重命名、CAS 和秘密合并耦合到通用协调器。为 OIDC、Webhook 分别建立专用 API 虽然更快，却会复制候选、审批、凭证和恢复逻辑。

## 决策

### 1. Profile 是独立配置资源，不是根配置中的动态秘密路径

插件在签名 `configuration` 中声明一个 `resourceController` 和 1—16 个 `resourceCollections`。集合拥有稳定语义 ID、`kind`、标题、闭合非敏感 JSON Schema、固定 `managedCredentials`、最小/最大数量；首个发布 kind 为 `profile`。

可信目录把语义集合 ID 派生为不透明 `cfgc_*`，资源实例使用随机 `cfgp_*`。浏览器和其他插件只引用不透明资源 ID，不直接使用插件 ID 或控制器路由。根配置仍独立管理启动参数，并继续选择 restart/hot 生效路径；动态 Profile 不迫使 `stateFile`、容量等启动参数伪装成可热变更字段。

### 2. 使用语言中立 `configuration.resource.v1`

新增 `configuration.resource-controller` 扩展点。其不透明 capability 由签名插件 ID 派生，运行策略只允许可确定唯一所有者的 leader-owned/leader，或 external-shared/queue。协议固定：

- `list/get`：返回签名 Schema 允许的非敏感 values、revision/digest 和凭证“已配置/版本”状态；不返回 handle、密文或 material；
- `prepare`：对 `create/update/delete` 执行 Active CAS、Schema、数量、引用和外部可用性校验，耐久准备但不切 Active；
- `commit/abort/status`：按 candidate/request digest 幂等提交、终止和恢复事实。

`create` 不得伪造旧 Active；`update/delete` 必须携带精确 Active revision/digest；提交删除后 Active 必须不存在。协议 Observation 只含摘要与生命周期事实，查询响应与控制响应保持分离。

### 3. 秘密按固定槽替换，缺省表示保留

每个集合在签名清单中固定秘密槽。创建时必须满足 required；更新时未提交的秘密槽由资源控制器从当前 Active 保留，新输入只产生 replacement CredentialRef；删除不接受新秘密。plugin-settings 不读取旧 handle，浏览器也不提交 CredentialRef。

替换顺序为：委托 stage → Candidate 可用 → 控制器 prepare/probe → 异人审批 → 控制器原子 commit → 委托凭证转 Active → 资源所有者退役被替换的旧版本。控制器提交后即使协调器短暂中断，Candidate 凭证仍可被可信 Material Lease 使用，恢复流程继续激活而不回退到明文或中断 Active。旧引用只由其认证 owner 退役。

### 4. 语言与进程选择

协议、Schema 和摘要是语言中立真源。Node 插件使用 Node SDK 并继续运行于当前共享 `node-worker`；资源控制器不是新增独立进程。首个纵切选择 Webhook Delivery：Node 的 HTTPS/AbortSignal 生态适合外部探测，流程又比 OIDC redirect/session 简单。OIDC 在相同协议稳定后迁移。

## 影响

- 正面：Profile 获得稳定身份、独立 CAS/审计/回滚；固定秘密槽可由可信宿主签发 ConfigurationAuthority；Workbench 可直接采用 MasterDetail；不同领域复用同一生命周期。
- 成本：新增资源控制器协议、目录字段、协调 Saga 与插件持久状态；引用 Profile 的现有配置需要改用 `cfgp_*`。
- 边界：本 ADR 不开放任意业务对象数据库，也不允许功能插件定义脚本化路径规则。新 kind 必须单独证明通用生命周期适用。

## 实施记录（2026-07-23）

- 已新增签名 `resourceController/resourceCollections`、闭合 Schema 和语义校验。
- 已新增 `configuration.resource.v1` Go wire、严格请求/响应校验、独立 create/update/delete 不变量与无 handle 查询视图。
- Backend 公开扩展点、运行时贡献合成和 Configuration Catalog 已识别不透明资源控制器与 `cfgc_*` 集合；浏览器视图裁剪真实路由目标。
- `ConfigurationAuthority` 已把授权精确绑定到 `cfgc_* + cfgp_* + field`；只允许认证后的凭证服务一次性消费，跨集合、跨资源和跨字段均拒绝。
- plugin-settings 0.9.0 已实现 create/update/delete Draft、Active CAS、委托凭证、prepare/commit/abort/status、异人审批、显式放弃和重启恢复；公开状态不含 handle、stage ID、请求摘要或控制器目标。
- Node `@vastplan/configuration-resource-controller-node` SDK 已实现闭合 wire、精确 caller、无 handle 查询裁剪与 Go/Node 摘要 golden；共享 node-worker 不增加 Profile 级进程。
- Webhook Delivery 0.2.0 已作为首个 Node 纵切：持久化租户隔离 Profile、Material Lease 探测、原子切换、旧引用退休补偿和 Delivery 热查表。因为当前状态是本地耐久文件，运行策略收紧为 `leader + leader-owned`。
- Node BFF、TypeScript Platform Admin SDK 和 Workbench MasterDetail 已接入固定资源路由与独立权限；浏览器不直接暴露插件 ID、控制器路由或 CredentialRef。
- 单元测试已覆盖跨租户拒绝、Active CAS、精确资源秘密授权、重启恢复、旧引用退休、跨语言摘要和 BFF 固定路由。
- 真实多服务验收完成：Backend Platform revision 23 的 12 个服务单元与受管服务 revision 2 的 1 个单元均收敛 Ready；Webhook 0.2.0 以自包含 ESM bundle 在共享 node-worker 中完成协议协商并注册业务贡献和资源控制器，plugin-settings 0.9.0 与访问策略 0.25.0 实际激活。Portal `/operations` 与 `/` 均返回 HTTP 200，状态接口 `ready=true`；固定 BFF 实际返回 Webhook 的不透明 `cfg_* / cfgc_*` 签名目录并通过资源控制器读取空集合。随后 Ctrl+C 优雅停止且退出码为 0；按既定决定未执行 soak。
