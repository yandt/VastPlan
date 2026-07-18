# Interaction Access Policy

`com.vastplan.foundation.security.interaction-access-policy` 是 Broker 的独立入口权限插件。

- 可信 `plugin`、`runner` 与 `system` 才能创建或取消交互；
- Portal/Mobile 代理的已认证 `user` 才能读取、呈现和响应；
- Broker 继续独占租户、来源、允许呈现面、响应主体、超时和单一终态的对象级校验；
- Broker 仅能回调 `kernel.config.get` 读取自己的部署配置。

它不承担 Portal Composer 的角色授权，详见 ADR-0055。
