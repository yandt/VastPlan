# ADR-0079：Frontend 采用框架无关多语种契约与设计系统桥接

- 状态：已接受
- 日期：2026-07-19

## 背景

Portal 功能插件必须能在 Arco、MUI 以及未来的 Runner/Mobile Renderer 之间复用。Arco 通过 `ConfigProvider.locale` 管理组件文案，MUI 通过 Theme locale 管理组件文案，日期组件可能另有 Provider，RJSF/AJV 又独立产生 Schema 标题和校验消息。若功能插件直接依赖任一框架或 React i18n 库，语言、回退和格式化就会形成多套真相源。

## 决策

1. `@vastplan/ui-contract` 定义 BCP-47 locale、方向、`LocalizedText`、插件语言资源和 Portal 语言策略，不依赖 React 或 UI 框架。
2. Portal Platform Profile 可固定 `defaultLocale` 与 `supportedLocales`；未声明时 Resolver 物化为 `zh-CN`、`en-US`。运行时按用户偏好、浏览器语言、平台默认的顺序选择，用户偏好按 tenant/portal 隔离保存。
3. 页面标题和导航在插件注册阶段只提交宿主绑定命名空间的消息描述符，Shell 在渲染时解析；切换语言不重建插件 Generation。
4. 插件资源随已验签、内容寻址的单文件 ESM 交付。宿主绑定真实插件 ID，限制 locale 数量、消息键、单条长度和总大小；资源不能携带 HTML、ReactNode 或可执行代码。
5. `@vastplan/portal-ui` 提供 React binding、Intl 日期/数字/列表/相对时间格式化和语言切换。Arco 与 MUI 适配器分别桥接自己的 Provider；功能插件只消费稳定契约。
6. JSON Schema 保持标准验证文档。`FormSchema.localization` 与 `uiLocalization` 通过 JSON Pointer 本地化渲染副本，不能改变原验证 Schema；RJSF/AJV 错误在适配器边界翻译。
7. locale 使用 `Intl.getCanonicalLocales` 规范化，精确匹配优先，其次同语言匹配，最后平台默认。契约从首版支持 `ltr/rtl`。
8. 所有进入 Portal 运行集合的 UI 插件都必须声明插件内语言资源；缺失默认语言、非法或规范化后重复的 locale、非法消息键会令 Portal 组装失败。第一方插件至少交付 `zh-CN` 与 `en-US`。纯后端插件不受此约束；UI 插件即使当前只贡献无文字 Slot，也保留最小语言资源，避免以后增加可见文案时绕过治理。
9. 用户可见文案归产生它的插件所有：内核不代管功能插件文案，功能插件也不能覆盖内核、布局或设计系统的命名空间。缺少非默认语言消息时允许回退，缺少插件语言资源本身则不允许启动。

## 结果

- 功能插件和跨端语义不绑定 i18next、FormatJS、Arco 或 MUI。
- 组件库内部文案、业务文案和动态表单共享一个 Portal locale，但由不同适配层消费。
- 新 UI 框架必须实现 locale/direction 桥接，不能建立第二个用户语言真源。
- 第一阶段内置 `zh-CN`、`en-US`；增加语言只需扩展 Platform Profile 允许列表和各插件资源，无需修改功能插件 API。
