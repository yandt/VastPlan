# 插件配置协调器

插件 ID：`cn.vastplan.platform.configuration.plugin-settings`

当前制品版本：`0.5.0`

该 platform 基础插件以 `leader + leader-owned + cluster + platform routing domain` 运行。它只通过宿主窄能力读取与活动 Deployment revision/digest 精确匹配的 `ConfigurationCatalog v1`，保存租户隔离的配置候选和审计；不依赖 Deployment Manager 存量状态，不读取工作区 Manifest，也不接受浏览器上传 Schema、插件身份或凭证 owner。

当前 capability `platform.plugin-configuration` 提供：

- `listDefinitions/getDefinition`：查询可信配置目录；
- `listCandidates`：查询候选及后续生效状态；
- `createDraft`：按 Catalog digest 与签名 JSON Schema 创建 Draft；非敏感 `values` 与只写 `secrets` 分离，秘密逐字段使用宿主一次性授权交给凭证托管器；
- `discardDraft`：以 revision CAS 进入回滚，终止该候选的全部委托凭证后才完成放弃。
- `submitDraft`：仅对 Application 来源的 restart 配置创建 candidate 绑定的 `PendingApproval` 服务修订；调用以 candidate ID 幂等。
- `activateCandidate`：仅在 Deployment Manager 已由不同主体批准后，打开候选凭证窗口、发布精确服务修订、等待 readiness，并提交凭证或执行单调回滚。

Draft 不会改变 Deployment、Platform Profile 或目标插件状态，也不会显示为 Active。Application 配置在独立审批、精确 Deployment 发布和 readiness 完成后才进入 Ready；失败候选创建新 revision 回滚，不能把 KV revision 倒退。`ConfigurationAuthority` 默认 45 秒有效且只能由凭证插件 CAS 消费一次；协调器状态只保存恢复 Saga 所需的 stage/ref，公开候选只返回字段状态，不返回 handle、stage ID、authority、密文或明文。状态使用私有目录、`0600` 原子文件、`fsync`、大小和候选数量上限。

Node Portal Kernel 已提供固定、无插件 ID 的 `/plugin-configurations` BFF 路由；Management Binding、在线角色和 CSRF 同时强制。Workbench 页面从定义返回的签名 Schema 动态准备表单，并把 `managedCredentials` 渲染为 `vastplan-secret-material + writeOnly` 字段；提交完成后由 Workbench 删除短时材料状态，功能插件不直接拼接基础 UI。

完整设计见《[插件配置与托管凭证](../architecture/插件配置与托管凭证.md)》、[ADR-0113](../decisions/ADR-0113-可信插件配置目录与分路径生效.md)、[ADR-0114](../decisions/ADR-0114-一次性ConfigurationAuthority与委托凭证暂存.md) 和 [ADR-0115](../decisions/ADR-0115-Application配置激活Saga与候选凭证窗口.md)。
