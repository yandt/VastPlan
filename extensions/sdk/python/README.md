# VastPlan Python 插件 SDK

Python SDK 与 Go SDK 使用同一份 `contracts/proto/` 契约，负责握手、双向 Channel、贡献注册、调用、生命周期、心跳、取消和事件发布。

开发环境安装：

```bash
python3 -m pip install -e extensions/sdk/python
```

插件入口只需创建 `Plugin`、登记 `Contribution`，最后调用 `serve()`。完整示例见 `extensions/plugins/com.vastplan.python-hello/backend/main.py`。

处理器收到的是宿主按受众裁剪后的统一 Wire `CallContext`。业务代码可用 `ContextViews.from_wire(call_context)` 读取与 Go SDK 同名的只读语义视图；清单 `contextAccess.required/optional/baggage` 只是申请，最终字段仍受扩展点、发布者和运行边界上限约束。委托引用由 SDK 在当前处理线程自动放入 HostCall 专用字段，不进入 `metadata`。

当前 Python 驱动属于第一方可信进程运行模式。未知发布者会被节点策略提升到至少 `process-sandbox`，在隔离驱动落地前拒绝启动，不会自动降级到可信进程。
