# ADR-0177 Portal 外观本地偏好与账户入口

- 状态：已采纳
- 日期：2026-07-31
- 取代：[ADR-0112](ADR-0112-PortalPreference服务端真源与候选提交事务.md) 中有关 Renderer、Shell、主题和图标服务端保存的部分；Workbench 集合偏好部分继续有效

## 背景

Portal 外观包含 Renderer、Shell 布局、明暗模式、主题模板、图标主题和自定义颜色。这些选择只影响当前用户、当前浏览器的显示，不属于企业组合事实、业务数据或跨设备协作状态。把外观混入服务端 `PortalPreference` 会引入 CAS 冲突、共享状态写放大、隐私暴露和不必要的服务可用性依赖，也使一次纯视觉切换穿过 BFF、Capability 和 Shared State 全链路。

同时，账户入口与系统管理混放会让导航职责不清：用户信息和个人外观属于账户域，系统管理属于平台一级导航。

## 决策

1. 所有外观选择统一交给可信 Portal Host 的 `PortalAppearanceSession`。它只读写浏览器 `localStorage`，不具备 HTTP、Capability、数据库或服务端缓存端口。
2. 存储键按 `tenant + authenticated subject + portal` 隔离，值使用有界、版本化、白名单结构。只接受活动 Profile 允许的 Renderer、Shell、主题和图标 ID；颜色只接受固定语义字段与六位十六进制值。
3. 服务端 `PortalPreference` 只保留 Workbench Collection 的列顺序、隐藏列、密度和分页大小。其 scope 只包含 Portal 与 Workbench catalog；服务端解析器明确拒绝 `rendererId`、`rendererOptions`、`shellTemplateId` 等外观字段。
   滚动重启期间，新浏览器内核可以接收旧 Portal Host 投影的 `renderer/shell` scope 字段，但必须先验证再丢弃；内部 scope 和后续偏好处理仍只有 `portalId + workbench`，不得因此恢复服务端外观语义。
4. Profile 继续定义默认值和允许目录，但不保存用户选择。Renderer 切换写入本地后刷新 Host Epoch；Shell Library 通过同一 Generation 管理器切换；主题、颜色和图标在当前 Generation 内更新。
5. 明暗模式支持跟随系统、浅色和深色。每个用户分别保存浅色/深色模板与语义色覆盖；跟随系统只监听 `prefers-color-scheme`，不向服务器发请求。
6. Shell 只显示一个圆形账户头像。头像是受治理 `account` 根导航分组的专用触发器，不拥有菜单清单或功能页面：标准侧栏点击头像后在右侧常驻二级导航面板加载该分组，顶部布局以同一组合模型的 Popover 承载，移动端进入同一 Drawer 导航。
7. “用户信息”和“用户设置/外观”由独立签名、独立版本的基础插件 `cn.vastplan.foundation.frontend.identity.account-center` 通过标准 `addPage` 契约注册。它因使用可信 Host 本地个性化窄端口而归入 `foundation.frontend`，不作为普通应用插件暴露；页面仍与其他插件共用加载、路由和 Generation 热替换链。Shell Composition 统一生成 `root group → child group → page` 与 `activeNavigationPath`。今后新增账户安全、会话或通知功能只增加页面贡献，不得修改头像组件或建立第二套二级菜单状态。
8. 企业用户、组织和角色管理属于管理员能力，应由独立管理插件注册到最后一个 `settings` 一级导航，不得混入个人账户入口。
9. Ant Design、Arco 和 MUI 通过统一 `themeTemplate + semantic themeColors` Provider 契约接收外观，不允许 Portal Host 针对框架编写分支。账户中心通过可信 `PortalPersonalizationProvider` 窄端口读取账户摘要和本地外观操作，不直接访问 Host Session、HTTP 或服务端偏好。

## 备选方案

- **外观继续以服务端偏好为真源**：可跨设备同步，但增加冲突、服务依赖与隐私面，且与纯本地显示语义不符，否决。
- **只把自定义颜色留在本地，Renderer/Shell 仍上服务端**：形成两套生命周期和故障语义，切换入口难以原子工作，否决。
- **每个 Renderer 自己管理 localStorage**：会复制隔离、校验、迁移和系统主题监听逻辑，且未来框架行为漂移，否决。

## 影响

- 外观切换不再触发服务端写入或 CAS 冲突，离线时仍完整可用，服务器无法收集用户外观选择。
- 用户在不同浏览器或设备上的外观不会自动同步；这是本决策接受的隐私与低耦合取舍。
- 清理浏览器站点数据会恢复 Profile 默认外观。企业管理员仍可通过 Profile 收窄允许目录，但不能读取或强制保存个人颜色。
- 个人中心插件未装配时，Shell 不显示空头像入口；装配后头像只是 `account` 根分组的特殊视觉入口，页面加载仍完全遵循统一插件机制。
- UI Contract 8.3.0 增加账户摘要、外观设置和语义颜色 Provider 输入；所有第一方 Renderer 与 Shell Library 必须同步升级。
