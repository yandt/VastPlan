# Portal Composer Plugin

`com.vastplan.platform.configuration.portal-composer` 是 Portal 在线组合与发布治理插件。管理中心分为 `Platform Profiles` 与 `Portals` 两个工作区；Portals 下分别管理 Applications、Bindings 和不可变 Activations。Application 草稿只编辑 Portal 路由、受众、品牌、非敏感配置和应用功能插件，不能替换 Profile 治理的设计系统、Shell 组合或布局。

Profile、Application 和 Binding 均执行 `Draft → PendingApproval → Approved → Published`，且 Published 只表示可被引用，不代表线上生效。Activation 精确引用三类 Published revision，服务端重新解析并锁定输入摘要、逐插件来源和管理绑定；随后依次执行输入校验、快照生成、Edge 就绪与 CAS 激活。可信内核 Catalog 复核精确制品、发布者分类、单一设计系统和 UI 契约，Composer 本身不接触仓库凭据或验签密钥。

只有 `PortalActivation` 是线上事实。成功 Activation 记录不可修改；更晚的成功记录会把旧记录投影为 `Superseded`。回滚引用历史 Activation 的精确输入创建一条新 Activation，不会复活或改写旧记录。请求必须携带 `expectedCurrentId`，并发管理员使用过期值时会被 CAS 拒绝。

该插件只负责呈现与提交意图；校验、双人审批、发布、Activation、回滚与审计均通过 Edge/BFF 的受保护 API 完成。浏览器不得直接获得服务凭据、内部服务地址或原始身份令牌。生产插件仅经已认证的 `kernel.config.get` 取得状态位置，并通过窄化的 Catalog 能力请求内核验证和物化制品。

```bash
pnpm --filter @vastplan/portal-composer typecheck
```

完整治理流程与安全边界见《[前端门户内核](../../../docs/dev/architecture/前端门户内核.md)》。
