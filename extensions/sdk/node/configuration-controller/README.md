# VastPlan Node Configuration Controller SDK

`@vastplan/configuration-controller-node` 把 Node 插件的配置状态机接入语言中立 `configuration.v1`。它负责：

- 从签名插件 ID 派生与 Go 完全一致的不透明 `configuration.*` capability；
- 严格解析 `prepare/commit/abort/status` wire，并拒绝未知字段与非托管凭证引用；
- 校验调用者必须是同租户的 plugin-settings；
- 生成与 Go 契约一致的 request/configuration digest；
- 校验并裁剪只包含摘要与生命周期事实的 Observation。

SDK 不保存状态，也不替插件实现原子提交。插件仍须把 Active 和 Candidate 持久化，并让四个操作幂等：

```js
import { Plugin } from "@vastplan/backend-plugin";
import { configurationControllerContribution } from "@vastplan/configuration-controller-node";

const plugin = new Plugin({ id: pluginId, version, engines: { backend: "^0.1" } });
plugin.contribute(configurationControllerContribution(pluginId, {
  async prepare(request, runtime) { /* validate + durable prepare */ },
  async commit(request, runtime) { /* atomic Active switch */ },
  async abort(request, runtime) { /* terminate uncommitted candidate */ },
  async status(request, runtime) { /* return durable facts */ },
}));
```

`runtime` 含本次 invocation、HostCall 端口与只读调用上下文。响应不得包含 values、CredentialRef、密文或 material。完整边界见 [ADR-0117](../../../../docs/dev/decisions/ADR-0117-语言中立Service-Hot配置控制器.md)。
