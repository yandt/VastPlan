# ADR-0119 Tenant 与 User Scoped Hot 配置真源

- 状态：已采纳，首个纵向闭环已实现
- 日期：2026-07-23
- 关联：[ADR-0113](ADR-0113-可信插件配置目录与分路径生效.md)、[ADR-0117](ADR-0117-语言中立Service-Hot配置控制器.md)、[ADR-0118](ADR-0118-独立配置资源与动态Profile.md)

## 背景

`service + hot` 的 Active 属于目标插件进程，独立 Profile 的 Active 属于资源控制器；`tenant/user + hot` 不同：同一组插件实例要按调用上下文解析多个租户或主体的值，Active 必须有一个可共享、可观察且受治理的唯一真源。

把这些值写入 `global-settings` 会形成第二套插件配置目录和 Schema 体系；把它们写入 Backend Kernel 又会让业务配置污染微内核。让浏览器或插件请求携带 tenant、subject、插件 ID 或目标配置 ID，则允许调用方自行扩大读取范围。

## 决策

### 1. plugin-settings 持有唯一 Active，内核不保存业务值

`cn.vastplan.platform.configuration.plugin-settings` 作为 leader-owned 平台插件保存 tenant 隔离的 Scoped Active、候选、异人审批和审计。当前 File State 是测试/单 leader 实现；未来可在不改变调用协议和管理 API 的前提下接入外部共享 Store Provider。`global-settings` 继续只管理普通平台 KV，内核只负责可信上下文、目录和协议分发。

Active 键由 `tenant + configurationId + subject` 的私有摘要形成。tenant 只来自 `CallContext.tenant_id`；tenant scope 的 subject 为空，user scope 的 subject 只来自 `CallContext.principal.user_id`。状态文件不保存 tenant 到公开候选，也不向运行时响应回显 tenant/subject。

### 2. 增加语言中立 `configuration.scoped.v1`

新增 single 扩展点 `configuration.scoped-resolver`，稳定 capability 为 `configuration.scoped`，固定操作：

- `resolve {}`：按认证 caller plugin、tenant 和可选 subject 找到唯一活动 Scoped 定义，返回非敏感 values、revision/digest、Schema/Artifact 摘要和来源；
- `watchRevision {afterRevision, afterDigest, timeoutMs?}`：有界等待，只返回 revision/digest 是否变化，不携带 values；调用方观察到变化后必须重新 `resolve`。

请求不接受 `configurationId`、plugin ID、tenant、subject、Schema、Artifact 或 URL。Resolver 从当前活动 `ConfigurationCatalog` 中按认证 caller plugin 精确查找；零个或多个定义都 fail-closed。响应每次重新校验当前目录、Schema、Artifact 和 Active 值，watch 事件不能替代权威读取。

签名清单声明 `tenant/user + hot` 时必须同时以 `remote + strong + readiness + fail` 依赖 `configuration.scoped`。协议是 JSON wire；Go SDK 只是首个 consumer adapter，Node、Python 和其他 Runtime 不需要 Go ABI。

### 3. Seed revision 0 与 Active CAS

活动 Catalog 中的非敏感 values 是签名部署 Seed，表示 revision `0`。首个候选绑定 Seed digest；后续候选绑定当前 Active revision/digest。激活在 plugin-settings 的同一次原子状态写入中完成：

```text
Draft -> Publishing/PendingApproval -> Publishing/Approved -> Ready
                         |                         |
                         +--------------------> RolledBack
```

审批人必须不同于创建/提交人。激活前重新读取活动 Catalog，复核配置 ID、Schema digest、Artifact SHA-256、scope 和值 Schema，并以 Active revision/digest CAS；漂移时不覆盖。提交成功后关闭旧 watch generation，运行实例重新 resolve。进程重启只恢复 PendingApproval/Approved 事实，不重放外部副作用。

### 4. 管理面与运行时端口分离

Portal 继续只调用固定 `platform.plugin-configuration` BFF。Scoped 发布使用独立 `platform.plugin-configuration.scoped.publish` 权限和四个操作；user scope 目标主体是管理对象，不是调用者自证身份。运行时插件只能调用 `configuration.scoped-resolver`，不能调用管理操作；用户不能直接调用 resolver。

首个阶段只开放无托管凭证 Scoped 定义。普通 Service Hot 的引用保留/替换，以及 Scoped Hot 的凭证引用投影，必须在完整旧引用退休和宿主运行身份绑定完成后另行接入，不能用非敏感协议伪装已支持。

## 语言与运行形态选择

- **Go（采用）**：复用现有 Catalog、JSON Schema、CAS、原子文件和审批状态机；低资源、并发 long-poll 简单，且不增加进程——能力进入既有 plugin-settings 独立可信进程。
- **Node.js**：适合 Portal/BFF 和未来 Node consumer SDK，但作为第二份 Active 状态机会复制强事务边界。
- **Python**：适合配置分析、策略建议或迁移工具，不适合首个低延迟、多并发 revision watch 真源。
- **Rust/Java 等**：可以实现未来 Store Provider，但当前引入成本大于收益；稳定 wire 已保留替换空间。

首个消费者使用 Go `hello-world` 0.2.0：`greet` 每次从 resolver 读取 tenant-scoped `greetingTemplate`，请求不能选择 tenant 或配置定义。该插件已位于实际 Application Composition，能够验证跨服务寻址、权限、Seed、Active 和热读取闭环。

## 备选方案

- **复用 global-settings**：失去签名 Schema、Artifact 绑定和候选审批，否决。
- **内核保存 Scoped Active**：污染微内核并固化存储实现，否决。
- **复用 tool.package**：把运行时内部读取端口混入产品/管理 API，否决。
- **插件提交 configurationId/tenant/subject**：扩大同插件多安装或跨主体选择面，否决。
- **watch 直接推送 values**：事件可能重放、越权或过期，否决；watch 只提示 revision 变化。

## 实施记录

- `contracts/schemas/configurationscoped/v1` 已提供闭合 Schema、严格请求解析、Seed/Active 响应校验和 canonical value digest。
- Backend 公开扩展点、descriptor Schema、Registry、兼容门禁和访问策略已登记 `configuration.scoped-resolver`。
- plugin-settings 0.10.0 已实现 tenant/user 隔离 Active、并行 user 候选键、异人审批、Active CAS、原子持久化、重启恢复、value-free 有界 watch、固定 BFF/TypeScript SDK 与 Workbench 操作。
- hello-world 0.2.0 已作为首个 tenant-scoped consumer 接入 Go SDK；Application Composition Seed 明确保存 `greetingTemplate`。
- 本地 fresh 纵向验收完成：Backend Platform 的 12 个单元与 managed-services 的 1 个单元均收敛 Ready，plugin-settings 0.10.0 实际注册 `configuration.scoped-resolver/configuration.scoped`，hello-world 0.2.0 在独立 managed node 以跨 Deployment 强依赖完成激活；前台持活时 `/operations` 与 `/` 均返回 HTTP 200，随后 Ctrl+C 优雅停止。按既定决定未执行 soak。
- 验收同时消除了两项集成漂移：platformdev 不再内嵌过期的 hello-world 版本，而读取独立受管服务组合；Deployment 发布校验不再把“当前 Deployment 中没有 provider”误判为“全局没有 provider”，带完整 `logicalService + routingDomain` 的外部依赖由 Node Agent readiness 继续 fail-closed。
- 2026-07-23：plugin-settings 0.12.0 已按 [ADR-0124](ADR-0124-Plugin-Settings租户聚合与Active-Active协调.md) 把原 File State 迁移到 tenant Shared State CAS，并切换为 active-active；上文 File State 描述仅保留为本 ADR 落地时的历史记录。Scoped watch 现以本实例即时通知加跨实例一秒观察保持 value-free 更新语义。
