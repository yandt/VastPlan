# VastPlan Python 插件 SDK

Python SDK 与 Go SDK 使用同一份 `contracts/proto/` 契约，负责握手、双向 Channel、贡献注册、调用、生命周期、心跳、取消和事件发布。

开发环境安装：

```bash
python3 -m pip install -e extensions/sdk/python
```

插件入口只需创建 `Plugin`、登记 `Contribution`，最后调用 `serve()`。完整示例见 `extensions/plugins/cn.vastplan.python-hello/backend/main.py`。

处理器收到的是宿主按受众裁剪后的统一 Wire `CallContext`。业务代码可用 `ContextViews.from_wire(call_context)` 读取与 Go SDK 同名的只读语义视图；清单 `contextAccess.required/optional/baggage` 只是申请，最终字段仍受扩展点、发布者和运行边界上限约束。委托引用由 SDK 在当前处理线程自动放入 HostCall 专用字段，不进入 `metadata`。

`ManagedCredentialRef` 提供闭合、不可变的托管凭证引用；它不读取或解密 material。`ScopedConfigurationClient` 消费身份无关的 `configuration.scoped.v1`，严格校验未知字段、Seed/Active 语义和值摘要。两者与 Go、Node SDK 共用 `contracts/testdata/sdk-interop-v1.json`，业务插件不要复制 validator。

`python-subinterpreter` 共享 Runtime 也支持处理器内同步 HostCall。opaque 委托令牌只保存在可信主解释器，子解释器只收到裁剪后的上下文和纯 Python target；桥接受父 deadline、显式取消、30 秒超时与 4 MiB payload 上限约束。

`SharedStateClient` 通过 `state.shared.v1` 使用宿主提供的 CAS 状态服务。插件只提交局部 namespace/key；tenant、插件与 Runtime scope 由宿主从认证上下文和启动身份生成。客户端不会直接取得 NATS 或数据库连接凭证。

当前 Python 驱动属于第一方可信进程运行模式。未知发布者会被节点策略提升到至少 `process-sandbox`，在隔离驱动落地前拒绝启动，不会自动降级到可信进程。
