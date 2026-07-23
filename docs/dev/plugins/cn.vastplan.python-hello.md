# cn.vastplan.python-hello

第一方 Python 参考插件，用于持续验证 Backend 宿主不依赖 Go 插件实现。

- 贡献：`tool.package/vastplan.python-hello`
- 操作：`echo`，返回输入文本、`runtime=python` 和当前租户
- 事件：每次成功调用发布 `python.hello.invoked`
- 驱动：`python`，最低隔离 `trusted-process`
- 必需协议能力：`channel.cancel.v1`、`event.publish.v1`

源码入口为 `extensions/plugins/cn.vastplan.python-hello/backend/main.py`。本地开发需先安装 `extensions/sdk/python/requirements.txt`，或执行 `python3 -m pip install -e extensions/sdk/python`。

插件私有依赖使用 PEP 751 `supply-chain/pylock.toml`。当前示例没有业务第三方包，因此锁中的 `packages` 为空；SDK、gRPC 和 Protobuf 属于 Runtime Host 基座。新增业务依赖时，必须同时在清单写入精确直接版本、把完整传递 wheel 放入 `supply-chain/python-wheels/`，并重新生成标准锁。

发布时使用通用打包工具且不传 `-backend-bin`，保留 Python 源入口，并从仓库根注入许可证与 NOTICE：

```bash
go run ./engineering/tools/pluginpackage \
  -source extensions/plugins/cn.vastplan.python-hello \
  -license-file LICENSE \
  -notice-file NOTICE \
  -out dist/cn.vastplan.python-hello.tar.gz
```

该插件只用于第一方可信运行。它不是第三方沙箱示例；未知发布者会因隔离等级不足被节点策略拒绝。
