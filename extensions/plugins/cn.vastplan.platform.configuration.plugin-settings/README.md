# VastPlan Plugin Settings

`cn.vastplan.platform.configuration.plugin-settings` 是通用插件配置协调器。它通过可信宿主读取 Backend Resolver 基于验签制品生成、且与活动 Deployment revision/digest 精确匹配的 `ConfigurationCatalog v1`，并保存配置候选与审计记录。

当前 `0.4.0` 实现：

- 只返回活动、已发布 Deployment 的可信配置定义；
- 使用不透明 `cfg_` 资源 ID，浏览器不提交插件身份或 Schema；
- 按目录摘要和签名 JSON Schema 校验非敏感值；
- 通过可信宿主签发、NATS CAS 一次性消费的 `ConfigurationAuthority`，把托管秘密交给凭证插件并派生目标 owner/purpose；
- 以租户隔离、单候选、CAS 和 `Preparing -> Draft` / `RollingBack -> RolledBack` 语义创建和放弃 Draft；
- 提供受 Management Binding、角色与 CSRF 保护的 Node BFF 和 Workbench 动态表单；
- 使用私有状态目录、`0600` 原子文件、`fsync` 和大小/数量上限；
- Workbench 将托管字段渲染为一次性 `secretMaterial`，协调器不持久化明文、authority，也不向浏览器返回凭证 handle；
- 不把 Draft 宣称为 Active。

后续版本接入 Application Deployment、Backend Platform Profile 与 `configuration.v1` 热配置控制器，然后再开放发布操作。当前 Workbench 明确把保存结果显示为 Draft，而不是 Active。

状态文件由本插件自己的部署配置 `platform.plugin-configuration.stateFile` 提供，不能从请求或环境变量指定。完整边界见 [ADR-0113](../../../docs/dev/decisions/ADR-0113-可信插件配置目录与分路径生效.md) 与 [ADR-0114](../../../docs/dev/decisions/ADR-0114-一次性ConfigurationAuthority与委托凭证暂存.md)。
