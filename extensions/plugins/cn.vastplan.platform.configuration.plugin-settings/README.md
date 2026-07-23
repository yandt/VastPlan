# VastPlan Plugin Settings

`cn.vastplan.platform.configuration.plugin-settings` 是通用插件配置协调器。它通过可信宿主读取 Backend Resolver 基于验签制品生成、且与活动 Deployment revision/digest 精确匹配的 `ConfigurationCatalog v1`，并保存配置候选与审计记录。

当前 `0.8.0` 实现：

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
- 首批无托管凭证的 Hot 候选先由目标插件耐久准备，再经异人审批并原子提交；中断后从目标 `status` 恢复，不盲目重放；
- 浏览器只看到控制器是否可用，不获得 capability、logical service、routing domain、request digest 或托管凭证 handle；
- 发布前把新凭证推进到 Candidate 窗口，精确 readiness 成功后转 Active，失败时自动发布单调回滚并终止候选凭证；
- 已配置的必填秘密可留空保留，浏览器只看到字段状态和版本；
- 不把 Draft、PendingApproval 或 Publishing 宣称为 Active。

带托管凭证的 Service Hot 在“保留旧引用 + 替换新引用”合并与摘要语义完成前保持 fail-closed，Workbench 显示不可用。后续版本继续接入该闭环，以及 `hot-scoped` 的 tenant/user 隔离真源与受认证 resolve/watch 端口。当前 Workbench 明确区分 Application/Platform/Hot 权限、Draft、外部审批、Catalog 激活、Activating、Ready 和回滚状态。

状态文件由本插件自己的部署配置 `platform.plugin-configuration.stateFile` 提供，不能从请求或环境变量指定。完整边界见 [ADR-0113](../../../docs/dev/decisions/ADR-0113-可信插件配置目录与分路径生效.md)、[ADR-0114](../../../docs/dev/decisions/ADR-0114-一次性ConfigurationAuthority与委托凭证暂存.md)、[ADR-0115](../../../docs/dev/decisions/ADR-0115-Application配置激活Saga与候选凭证窗口.md)、[ADR-0116](../../../docs/dev/decisions/ADR-0116-Backend-Platform-Profile候选Catalog与配置激活.md) 与 [ADR-0117](../../../docs/dev/decisions/ADR-0117-语言中立Service-Hot配置控制器.md)。
