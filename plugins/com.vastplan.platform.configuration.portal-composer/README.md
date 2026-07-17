# Portal Composer Plugin

`com.vastplan.platform.configuration.portal-composer` 是系统配置与插件在线组合的前端参考插件。它把 Portal 名称、路由、设计系统和功能插件组合为草稿表单，并只依赖 `@vastplan/portal-ui`，因此不绑定 Arco 或后续任何特定 UI 框架。

该插件只负责呈现与提交意图；草稿校验、提交、双人审批、发布、回滚与审计必须通过 Edge/BFF 的受保护 API 完成。浏览器不得直接获得服务凭据、内部服务地址或原始身份令牌。

后端治理逻辑在 `portalcomposer/`：每一次 revision 都持久化并追加审计事件；`Draft → PendingApproval → Approved → Published` 禁止自审；同租户已发布 Portal 的路由和域名不能冲突。`system` break-glass 发布/回滚必须提供理由，并写入高优先级审计事件。制品目录校验通过 `Catalog` 接口注入，因此浏览器提交的插件 ID 不能绕过制品信任检查。

```bash
pnpm --filter @vastplan/portal-composer typecheck
```

完整治理流程与安全边界见《[前端门户内核](../../docs/dev/architecture/前端门户内核.md)》。
