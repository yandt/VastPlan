# Portal Composer Plugin

`com.vastplan.platform.configuration.portal-composer` 是系统配置与插件在线组合的前端参考插件。普通草稿只编辑 Portal 路由、受众、品牌、非敏感配置和应用功能插件；设计系统及平台基础插件由环境绑定的 Frontend Platform Profile 注入，不能由应用草稿替换。

发布端执行服务端双输入解析，生成包含 Platform Profile 与 Application Composition 摘要、逐插件来源和 Portal revision 的锁定结果。可信内核 Catalog 复核精确制品、发布者分类、单一设计系统和 UI 契约，Composer 本身不接触仓库凭据或验签密钥。

该插件只负责呈现与提交意图；草稿校验、提交、双人审批、发布、回滚与审计必须通过 Edge/BFF 的受保护 API 完成。浏览器不得直接获得服务凭据、内部服务地址或原始身份令牌。

后端治理逻辑在 `portalcomposer/`：每一次 revision 都持久化并追加审计事件；`Draft → PendingApproval → Approved → Published` 禁止自审；同租户已发布 Portal 的路由和域名不能冲突。`system` break-glass 发布/回滚必须提供理由，并写入高优先级审计事件。生产插件仅经已认证的 `kernel.config.get` 取得状态文件位置，并经 `kernel.portal.catalog.validate` 请求内核验证制品；它不会获得制品仓库凭据、验签密钥或可绕过信任的目录实现。

```bash
pnpm --filter @vastplan/portal-composer typecheck
```

完整治理流程与安全边界见《[前端门户内核](../../docs/dev/architecture/前端门户内核.md)》。
