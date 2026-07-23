# VastPlan Plugin Settings

`cn.vastplan.platform.configuration.plugin-settings` 是通用插件配置协调器。它通过可信宿主读取 Backend Resolver 基于验签制品生成、且与活动 Deployment revision/digest 精确匹配的 `ConfigurationCatalog v1`，并保存配置候选与审计记录。

当前 `0.11.0` 实现：

- 只返回活动、已发布 Deployment 的可信配置定义；
- 使用不透明 `cfg_` 资源 ID，浏览器不提交插件身份或 Schema；
- 按目录摘要和签名 JSON Schema 校验非敏感值；
- 通过可信宿主签发、NATS CAS 一次性消费的 `ConfigurationAuthority`，把托管秘密交给凭证插件并派生目标 owner/purpose；
- 以租户隔离、单候选、CAS 和 `Preparing -> Draft` / `RollingBack -> RolledBack` 语义创建和放弃 Draft；
- 提供受 Management Binding、角色与 CSRF 保护的 Node BFF 和 Workbench 动态表单；
- 使用私有状态目录、`0600` 原子文件、`fsync` 和大小/数量上限；
- Workbench 将托管字段渲染为一次性 `secretMaterial`，协调器不持久化明文、authority，也不向浏览器返回凭证 handle；
- Application 来源 restart 配置可提交为独立服务修订，由不同主体在 Deployment Manager 审批后，再从本页面执行专用发布；
- Platform Profile 来源 restart 配置使用独立权限与候选 Catalog：只能修改活动 Profile 的独立 service，不能编辑 attachment、共享 Profile 全文或其他 binding；
- Profile 配置支持提交、异人审批、发布、显式放弃和重启恢复；readiness 失败时单调回滚 Catalog 与 Deployment；
- Service Hot 配置通过签名清单派生的专用 `configuration.controller` 目标执行 `configuration.v1 prepare/commit/abort/status`，不复用产品工具 API；
- Service Hot 固定托管字段支持留空保留和新值替换：目标控制器以完整合并引用集计算摘要并原子提交，Candidate 凭证随后转 Active；提交与激活之间中断时从目标 `status` 恢复，不产生提交失败后的孤儿 Active 凭证；
- 目标控制器私有持久化旧引用退役 outbox；公开目录只投影配置状态和版本，不返回 handle；
- 浏览器只看到控制器是否可用，不获得 capability、logical service、routing domain、request digest 或托管凭证 handle；
- Tenant/User Scoped Hot 使用独立 `configuration.scoped-resolver`：运行时请求不接受配置 ID、插件 ID、tenant 或 subject，只按认证 caller 与上下文解析唯一签名定义；
- Scoped 候选支持 Seed revision 0、目标主体隔离、异人审批、Active CAS、原子持久化、重启恢复和不携带 values 的有界 watch；
- 发布前把新凭证推进到 Candidate 窗口，精确 readiness 成功后转 Active，失败时自动发布单调回滚并终止候选凭证；
- 已配置的必填秘密可留空保留，浏览器只看到字段状态和版本；
- 动态 Profile 使用独立 `cfgc_*` 集合与 `cfgp_*` 资源身份，支持 create/update/delete 草稿、Active CAS、异人审批、prepare/commit/abort/status 恢复与独立资源权限；
- Profile 固定秘密槽使用精确到集合、资源和字段的 `ConfigurationAuthority`，更新未填写的秘密保留旧引用，新值只产生 replacement；控制器提交后才激活新凭证，并由资源 owner 退役旧版本；
- Portal 提供独立 Workbench MasterDetail 页面；签名 Schema 动态生成资源表单，浏览器不获得控制器目标、凭证 handle、密文或 material；
- 不把 Draft、PendingApproval 或 Publishing 宣称为 Active。

带托管凭证的 Scoped Hot 仍保持 fail-closed；Service Hot 与独立资源 Profile 已完成“保留旧引用 + 替换新引用”闭环。当前 Workbench 明确区分 Application/Platform/Service Hot/Scoped Hot/Resource 权限、Draft、外部审批、Activating、Ready 和回滚状态。

状态文件由本插件自己的部署配置 `platform.plugin-configuration.stateFile` 提供，不能从请求或环境变量指定。完整边界见 [ADR-0113](../../../docs/dev/decisions/ADR-0113-可信插件配置目录与分路径生效.md)、[ADR-0114](../../../docs/dev/decisions/ADR-0114-一次性ConfigurationAuthority与委托凭证暂存.md)、[ADR-0115](../../../docs/dev/decisions/ADR-0115-Application配置激活Saga与候选凭证窗口.md)、[ADR-0116](../../../docs/dev/decisions/ADR-0116-Backend-Platform-Profile候选Catalog与配置激活.md)、[ADR-0117](../../../docs/dev/decisions/ADR-0117-语言中立Service-Hot配置控制器.md)、[ADR-0118](../../../docs/dev/decisions/ADR-0118-独立配置资源与动态Profile.md)、[ADR-0119](../../../docs/dev/decisions/ADR-0119-Tenant与User-Scoped-Hot配置真源.md) 与 [ADR-0120](../../../docs/dev/decisions/ADR-0120-Service-Hot托管凭证提交与退役.md)。
