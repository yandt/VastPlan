# 插件配置协调器

插件 ID：`cn.vastplan.platform.configuration.plugin-settings`

当前制品版本：`0.9.0`

该 platform 基础插件以 `leader + leader-owned + cluster + platform routing domain` 运行。它只通过宿主窄能力读取与活动 Deployment revision/digest 精确匹配的 `ConfigurationCatalog v1`，保存租户隔离的配置候选和审计；不依赖 Deployment Manager 存量状态，不读取工作区 Manifest，也不接受浏览器上传 Schema、插件身份或凭证 owner。

当前 capability `platform.plugin-configuration` 提供：

- `listDefinitions/getDefinition`：查询可信配置目录；
- `listCandidates`：查询候选及后续生效状态；
- `createDraft`：按 Catalog digest 与签名 JSON Schema 创建 Draft；非敏感 `values` 与只写 `secrets` 分离，秘密逐字段使用宿主一次性授权交给凭证托管器；
- `discardDraft`：以 revision CAS 进入回滚，终止该候选的全部委托凭证后才完成放弃。
- `submitDraft`：仅对 Application 来源的 restart 配置创建 candidate 绑定的 `PendingApproval` 服务修订；调用以 candidate ID 幂等。
- `activateCandidate`：仅在 Deployment Manager 已由不同主体批准后，打开候选凭证窗口、发布精确服务修订、等待 readiness，并提交凭证或执行单调回滚。
- `submitProfileDraft/approveProfileCandidate/activateProfileCandidate/abortProfileCandidate`：使用独立 `platform.plugin-configuration.profile.publish` 权限管理 Platform Profile restart 配置；目标 Profile、Catalog revision 和 service 只能由可信内核从活动目录推导。失败时先生成更高 Catalog 回滚修订，再发布更高 Deployment 回滚修订。
- `submitHotServiceDraft/approveHotServiceCandidate/activateHotServiceCandidate/abortHotServiceCandidate`：使用独立 `platform.plugin-configuration.hot.publish` 权限驱动目标插件的 `configuration.v1` 控制器；目标由签名 Catalog 派生，候选必须先耐久 prepare、再异人审批、最后原子 commit，进程中断后以 status 恢复。带秘密的普通 Service Hot 定义在引用合并语义完成前 fail-closed。
- `listResourceItems/getResourceItem`：按签名集合读取独立 Profile 的非敏感值、Active revision/digest 和凭证配置状态，不返回 handle。
- `createResourceDraft/updateResourceDraft/deleteResourceDraft`：以 `cfgc_* / cfgp_*` 和 Active CAS 创建资源候选；秘密只能写入签名固定槽，更新缺省保留旧引用，删除不接受新秘密。
- `submitResourceDraft/approveResourceCandidate/activateResourceCandidate/abortResourceCandidate`：使用独立 `platform.plugin-configuration.resource.publish` 权限驱动 `configuration.resource.v1`，执行耐久 prepare、异人审批、原子 commit、凭证激活和中断恢复。

Draft 不会改变 Deployment、Platform Profile 或目标插件状态，也不会显示为 Active。Application 和 Platform Profile 配置都在独立审批、精确 Deployment 发布和 readiness 完成后才进入 Ready；平台级路径额外要求 Catalog candidate 完成。失败候选只创建更高 revision 补偿，不能把 Catalog、Deployment 或 KV revision 倒退。`ConfigurationAuthority` 默认 45 秒有效且只能由凭证插件 CAS 消费一次；协调器状态只保存恢复 Saga 所需的 stage/ref 和 apply path，公开候选只返回字段状态，不返回 handle、stage ID、authority、密文或明文。状态使用私有目录、`0600` 原子文件、`fsync`、大小和候选数量上限。

Node Portal Kernel 已提供固定、无插件 ID 的 `/plugin-configurations` BFF 路由；Management Binding、在线角色和 CSRF 同时强制。Workbench 根配置页和 Profile MasterDetail 页都从签名 Schema 动态准备表单，并把 `managedCredentials` 渲染为 `vastplan-secret-material + writeOnly` 字段；提交完成后由 Workbench 删除短时材料状态，功能插件不直接拼接基础 UI。页面只显示控制器可用性和资源事实，不返回内部路由目标。

完整设计见《[插件配置与托管凭证](../architecture/插件配置与托管凭证.md)》、[ADR-0113](../decisions/ADR-0113-可信插件配置目录与分路径生效.md)、[ADR-0114](../decisions/ADR-0114-一次性ConfigurationAuthority与委托凭证暂存.md)、[ADR-0115](../decisions/ADR-0115-Application配置激活Saga与候选凭证窗口.md)、[ADR-0116](../decisions/ADR-0116-Backend-Platform-Profile候选Catalog与配置激活.md)、[ADR-0117](../decisions/ADR-0117-语言中立Service-Hot配置控制器.md) 和 [ADR-0118](../decisions/ADR-0118-独立配置资源与动态Profile.md)。
