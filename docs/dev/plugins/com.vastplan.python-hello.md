# com.vastplan.python-hello

第一方 Python 参考插件，用于持续验证 Backend 宿主不依赖 Go 插件实现。

- 贡献：`tool.package/vastplan.python-hello`
- 操作：`echo`，返回输入文本、`runtime=python` 和当前租户
- 事件：每次成功调用发布 `python.hello.invoked`
- 驱动：`python`，最低隔离 `trusted-process`
- 必需协议能力：`channel.cancel.v1`、`event.publish.v1`

源码入口为 `plugins/com.vastplan.python-hello/backend/main.py`。本地开发需先安装 `sdk/python/requirements.txt`，或执行 `python3 -m pip install -e sdk/python`。

发布时使用通用打包工具且不传 `-backend-bin`，保留 Python 源入口，并从仓库根注入许可证与 NOTICE：

```bash
go run ./tools/pluginpackage \
  -source plugins/com.vastplan.python-hello \
  -license-file LICENSE \
  -notice-file NOTICE \
  -out dist/com.vastplan.python-hello.tar.gz
```

该插件只用于第一方可信运行。它不是第三方沙箱示例；未知发布者会因隔离等级不足被节点策略拒绝。
