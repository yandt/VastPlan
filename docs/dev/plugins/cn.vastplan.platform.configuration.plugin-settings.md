# 插件配置协调器

插件 ID：`cn.vastplan.platform.configuration.plugin-settings`

当前制品版本：`0.3.0`

该 platform 基础插件以 `leader + leader-owned + cluster + platform routing domain` 运行。它只通过宿主窄能力读取与活动 Deployment revision/digest 精确匹配的 `ConfigurationCatalog v1`，保存租户隔离的配置候选和审计；不依赖 Deployment Manager 存量状态，不读取工作区 Manifest，也不接受浏览器上传 Schema、插件身份或凭证 owner。

当前 capability `platform.plugin-configuration` 提供：

- `listDefinitions/getDefinition`：查询可信配置目录；
- `listCandidates`：查询候选及后续生效状态；
- `createDraft`：按 Catalog digest 与签名 JSON Schema 创建非敏感 Draft；
- `discardDraft`：以 revision CAS 放弃尚未发布的 Draft。

`0.1.0` 尚未开放 publish。Draft 不会改变 Deployment、Platform Profile 或目标插件状态，也不会显示为 Active；托管秘密输入要等 `ConfigurationAuthority` 与 delegated credential stage 落地后才接入。状态使用私有目录、`0600` 原子文件、`fsync`、大小和候选数量上限。

Node Portal Kernel 已提供固定、无插件 ID 的 `/plugin-configurations` BFF 路由；Management Binding、在线角色和 CSRF 同时强制。Workbench 页面从定义返回的签名 Schema 动态准备表单，功能插件不直接拼接基础 UI。带 `managedCredentials` 的定义当前只显示字段数量，不渲染秘密输入，避免在 Owner Delegation 尚未落地时形成不安全半实现。

完整设计见《[插件配置与托管凭证](../architecture/插件配置与托管凭证.md)》和 [ADR-0113](../decisions/ADR-0113-可信插件配置目录与分路径生效.md)。
