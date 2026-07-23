# VastPlan Node Configuration Controller SDK

`@vastplan/configuration-controller-node` 把 Node 插件的配置状态机接入语言中立 `configuration.v1`。它负责：

- 从签名插件 ID 派生与 Go 完全一致的不透明 `configuration.*` capability；
- 严格解析 `prepare/commit/abort/status` wire，并拒绝未知字段与非托管凭证引用；
- 校验调用者必须是同租户的 plugin-settings；
- 生成与 Go 契约一致的 request/configuration digest；
- 合并 Active 凭证引用与本次 replacement，并计算旧引用退役集合；
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

`prepare.managedCredentials` 只包含本候选新暂存的 replacement，不是完整快照。控制器必须用 `mergeManagedCredentials(active, replacements)` 保留未填写的旧引用，以合并后的完整引用集计算 configuration digest；提交时把 `replacedManagedCredentials(active, merged)` 作为私有退役 outbox 持久化，并在 Active 切换后幂等重试。两类 helper 的返回值都不得进入 Observation、日志或普通配置响应。

`runtime` 含本次 invocation、HostCall 端口与只读调用上下文。响应不得包含 values、CredentialRef、密文或 material。完整边界见 [ADR-0117](../../../../docs/dev/decisions/ADR-0117-语言中立Service-Hot配置控制器.md)。
