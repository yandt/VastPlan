# VastPlan Authentication Broker

该 Foundation 插件按已发布 `AuthenticationProviderCatalog` 把门户认证请求路由到唯一 Provider，并锁定 transaction → Provider 所有权。它不保存企业用户、密码、角色或 Provider Secret，也不能签发权限。

Provider Profile、Lifecycle 和已发布 Catalog 由 `VASTPLAN_AUTHENTICATION_PROVIDER_STATE` 指定的 owner-only CAS 状态文件统一保存。`createDraft → validate → recordTest → approve → publish` 使用同一 generation；测试人与批准人必须不同，只有 Approved 且 Ready 的 Profile 才能与 Portal Binding 原子发布。Broker 直接读取该已发布视图，并使用 leader-owned 运行模型避免多实例对短时事务产生不同解释。

管理中心测试 Validated、尚未发布的 Profile 时，可信 Portal 通过 `beginProviderTest` 创建 `authentication-provider-test` 隔离事务。成功 Assertion 在服务端密封 Cookie 中短暂转交给 `recordTest`，不向浏览器暴露，也不允许粘贴或自报“测试成功”。正常登录和 Provider 测试的 Assertion 都必须由 leader-routed Broker 原子消费。

语言选择为 Go，因为该组件主要是有界路由、严格 Schema 校验和短时状态机；OIDC 等协议 SDK 仍放在独立 Provider 中使用更合适的语言。

架构见《[企业身份与种子访问](../../../docs/dev/architecture/企业身份与种子访问.md)》和《[登录与认证协议](../../../docs/dev/architecture/登录与认证协议.md)》。
