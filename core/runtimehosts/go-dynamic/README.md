# Go Dynamic Runtime Host

该进程是首方 dynamic-go 插件的受信任 Runtime Provider。Backend 只负责选择 Provider、
签发逐插件启动票据和管理 Pool lease；本目录下的 `loader` 是生产代码中唯一允许调用
Go `plugin.Open` 的包。

## 运行边界

- 仅接受父进程通过 stdin/stdout JSON 行控制通道发送的 `start`、`stop`、`shutdown`。
- 每个 `.so` 模块拥有独立的 protocolbus session、环境快照、贡献和生命周期。
- 共享只发生在相同服务、隔离等级、发布者信任域、ABI/构建指纹和 generation 内。
- Go 模块不可卸载；升级使用新 generation，候选接入后再排空旧 Host。
- 只允许经过签名、共同构建并满足首方硬身份的插件；第三方不得进入该 Provider。

## 构建与探测

使用仓库统一脚本共同构建 Runtime Host、插件模块和发布清单：

```bash
OUT_DIR=bin/dynamic-go ./engineering/tools/build-dynamic-go.sh
bin/dynamic-go/vastplan-go-dynamic-host --probe
```

开发编排器通过 `VASTPLAN_DYNAMIC_GO_HOST` 把 Host 的绝对路径交给 Backend。生产发布物
必须把该 Host 作为内核可信发布物安装，不能从插件包下载或由插件参数替换。
