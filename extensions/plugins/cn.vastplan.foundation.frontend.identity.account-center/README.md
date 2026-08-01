# VastPlan 个人中心插件

`cn.vastplan.foundation.frontend.identity.account-center` 是一个独立签名、独立版本的 Portal 基础插件。它通过与其他插件相同的页面、导航、路由和热替换契约提供：

- 用户信息页面；
- 用户设置分组；
- 仅保存在当前浏览器、修改后即时生效的外观设置页面；页面使用纵向分段，按“偏好设置 → 浅色/深色主题 → 颜色微调”组织，主题模板通过可替换的专用选择器选择。

插件不拥有 Shell 布局。它被归类为 `foundation.frontend`，因为外观页面需要可信 Host 的本地个性化窄端口和语义 UI 原语；它不作为普通应用插件暴露。Shell 只把统一组合模型中的 `account` 根分组渲染成专用账户入口：标准布局位于左下角，顶部布局位于右上角。

插件拥有版本化扩展点 `cn.vastplan.foundation.frontend.identity.account-center.page@1.0.0`。其他插件如需增加账户安全、会话管理或通知偏好页面，必须同时：

1. 在 Manifest `dependencies` 中依赖本插件；
2. 通过 Manifest `extensions` 声明页面 ID、目标分组和扩展契约范围；
3. 在自身前端入口用 Workbench 数据契约注册同一页面。

可信 Catalog 会在发布前校验所有者版本、扩展契约和 descriptor，Portal Runtime 会在同一 Generation 中核对实际页面、贡献插件和导航分组。未声明扩展关系的插件不能直接把页面挂入 `account` 或 `account.settings`。插件之间不得导入源码、共享 React 节点或借用主插件权限。

每个 Frontend Platform Profile 必须通过 `accountCenter` 选择一个个人中心实现，并在平台 `plugins` 中精确包含同一制品引用。默认 Profile 选择本插件；应用插件配置不能删除、降级或覆盖它。未来替换实现时只更换 Profile 中的语义选择，不需要修改 Portal 内核或 Shell。

企业级用户、组织和角色管理不属于个人中心，应由独立管理插件注册到 `settings` 区域。

```bash
pnpm --filter @vastplan/account-center typecheck
pnpm --filter @vastplan/account-center test
```
