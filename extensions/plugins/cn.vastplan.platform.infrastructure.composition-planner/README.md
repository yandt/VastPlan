# Application Composition Planner

`cn.vastplan.platform.infrastructure.composition-planner` 把用户可写的 Backend `ApplicationIntent` 编译为只读的 `ApplicationComposition`、Artifact Lock、Configuration Plan、Provider Binding 和 Service DAG。

它是无状态平台基础插件，不是内核组件，也不拥有 Deployment CAS、NATS KV、SSH、凭证 material 或仓库签名密钥。插件只通过 `platform.artifacts.repository` 的 `resolve` 与 `describePlanning` 操作读取已验证制品元数据；最终发布仍由 Backend 内核重新复验。

当前实现使用 Go：确定性图算法、SemVer、JSON Schema 与现有 Go 契约生态更成熟，常驻资源低。运行时默认使用已有 `native + trusted-process`；部署方允许第一方 dynamic-go 时可在不改变 capability 的情况下切换，Planner 本身不要求独立进程。

启动配置：

```json
{
  "channel": "stable",
  "kernelVersion": "0.1.0",
  "platform": "linux/amd64",
  "allowedChannels": ["stable"],
  "allowedPublishers": ["vastplan", "example"],
  "allowedPluginPrefixes": ["cn.vastplan", "com.example"],
  "allowDevelopmentPlugins": false
}
```

channel 顺序是仓库选择优先级；其他策略集合会规范排序并绑定到 Resolution Report 的 `planner.configurationDigest`。
