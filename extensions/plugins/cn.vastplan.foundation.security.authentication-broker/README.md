# VastPlan Authentication Broker

该 Foundation 插件按已发布 `AuthenticationProviderCatalog` 把门户认证请求路由到唯一 Provider，并锁定 transaction → Provider 所有权。它不保存企业用户、密码、角色或 Provider Secret，也不能签发权限。

Provider Profile、Lifecycle 和已发布 Catalog 由 service-scope fenced Shared State CAS 统一保存；生产组合通过 SQL Shared State 落入平台控制数据库，状态文件不再接受在线写入。`createDraft → validate → recordTest → approve → publish` 使用同一 generation，且该 generation 与 Shared State revision 必须一致；测试人与批准人必须不同，只有 Approved 且 Ready 的 Profile 才能与 Portal Binding 原子发布。Broker 在每次可信调用中绑定同一存储端口，Store 不可用、revision 漂移或 CAS 冲突时均 fail-closed。短时认证事务和一次性 Assertion 继续留在 Leader 有界内存中，不混入长期配置状态。

Seed 首次配置仍允许读取一份 owner-only Bootstrap Catalog，以便平台控制数据库尚未初始化时完成 Seed 登录；它只在 Shared State 明确“从未发布 Catalog”时回退。数据库超时、认证失败、损坏或其他不可用错误绝不回退 JSON，必须进入恢复模式。

该插件的 unit `stateModel` 继续标记为 `leader-owned`，因为认证 transaction 与 Assertion 一次性消费仍由 Leader 拥有，并且 Seed Access Provider 必须与 Broker 同单元。这个调度标签不表示 Provider 管理数据继续写文件；长期 Catalog 真相源已经是外部 Shared State。

管理中心测试 Validated、尚未发布的 Profile 时，可信 Portal 通过 `beginProviderTest` 创建 `authentication-provider-test` 隔离事务。成功 Assertion 在服务端密封 Cookie 中短暂转交给 `recordTest`，不向浏览器暴露，也不允许粘贴或自报“测试成功”。正常登录和 Provider 测试的 Assertion 都必须由 leader-routed Broker 原子消费。

语言选择为 Go，因为该组件主要是有界路由、严格 Schema 校验和短时状态机；OIDC 等协议 SDK 仍放在独立 Provider 中使用更合适的语言。

架构见《[企业身份与种子访问](../../../docs/dev/architecture/企业身份与种子访问.md)》和《[登录与认证协议](../../../docs/dev/architecture/登录与认证协议.md)》。
