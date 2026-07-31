# VastPlan 个人中心插件

`cn.vastplan.foundation.frontend.identity.account-center` 是一个独立签名、独立版本的 Portal 基础插件。它通过与其他插件相同的页面、导航、路由和热替换契约提供：

- 用户信息页面；
- 用户设置分组；
- 仅保存在当前浏览器的外观设置页面。

插件不拥有 Shell 布局。它被归类为 `foundation.frontend`，因为外观页面需要可信 Host 的本地个性化窄端口和语义 UI 原语；它不作为普通应用插件暴露。Shell 只把统一组合模型中的 `account` 根分组渲染成专用账户入口：标准布局位于左下角，顶部布局位于右上角。今后增加安全设置、会话管理或通知偏好时，应继续注册到 `account` 或其受治理子分组，不得在头像组件中硬编码功能清单。

每个 Frontend Platform Profile 必须通过 `accountCenter` 选择一个个人中心实现，并在平台 `plugins` 中精确包含同一制品引用。默认 Profile 选择本插件；应用插件配置不能删除、降级或覆盖它。未来替换实现时只更换 Profile 中的语义选择，不需要修改 Portal 内核或 Shell。

企业级用户、组织和角色管理不属于个人中心，应由独立管理插件注册到 `settings` 区域。

```bash
pnpm --filter @vastplan/account-center typecheck
pnpm --filter @vastplan/account-center test
```
