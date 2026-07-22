# VastPlan Authentication Broker

该 Foundation 插件按已发布 `AuthenticationProviderCatalog` 把门户认证请求路由到唯一 Provider，并锁定 transaction → Provider 所有权。它不保存企业用户、密码、角色或 Provider Secret，也不能签发权限。

当前静态适配器从 `VASTPLAN_AUTHENTICATION_PROVIDER_CATALOG` 读取不可被 group/other 写入的绝对 JSON 文件；在线控制面以后实现同一 `Catalog` 窄接口。Broker 使用 leader-owned 运行模型，避免多实例对短时事务产生不同解释。

语言选择为 Go，因为该组件主要是有界路由、严格 Schema 校验和短时状态机；OIDC 等协议 SDK 仍放在独立 Provider 中使用更合适的语言。

架构见《[企业身份与种子访问](../../../docs/dev/architecture/企业身份与种子访问.md)》和《[登录与认证协议](../../../docs/dev/architecture/登录与认证协议.md)》。
