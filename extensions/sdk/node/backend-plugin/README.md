# VastPlan Node Backend 插件 SDK

该 SDK 供第一方 ESM 插件在受信任 Node Worker Runtime 中接入统一 Plugin-Host 协议。它复用与 Go、Python 相同的握手、贡献、调用、生命周期、取消和事件契约。

插件清单必须显式声明：

```json
{
  "execution": {
    "backend": {
      "driver": "node-worker",
      "minimumIsolation": "trusted-runtime",
      "node": { "workerSafe": true, "moduleFormat": "esm" }
    }
  }
}
```

Node Worker 是可释放的生命周期边界，不是第三方代码安全沙箱。未知发布者仍会被节点策略拒绝，或改由 `process-sandbox`、`container`、`wasm-component` 驱动承载。
