# ADR-0137：Python 标准依赖锁与离线 Overlay

状态：Accepted（2026-07-24）

## 背景

Python 插件原先只在 `execution.backend.requirements` 记录直接依赖。它不能表达传递依赖、环境 marker、wheel 文件、大小和摘要，Node Agent 启动时也依赖机器全局 Python 环境。因此相同插件包在不同节点可能解析到不同依赖，断网节点不能安装，SBOM 也只能反映作者直接填写的版本。

PyPA 已通过 [PEP 751 / `pylock.toml` 规范](https://packaging.python.org/en/latest/specifications/pylock-toml/)，`pylock.toml` 1.0 是可复现 Python 环境的标准锁格式。[pip lock](https://pip.pypa.io/en/stable/cli/pip_lock/) 已能生成该格式，但当前仍标为实验性且只保证当前 Python/平台；生成命令属于构建侧能力，不能成为生产节点的在线解析器。

## 决策

1. Python 插件必须携带 `supply-chain/pylock.toml`。打包器把它作为 `supplyChain.pythonLock` 写入最终 Manifest，绑定格式 `pylock-toml`、规范 `1.0`、固定路径和精确 SHA-256；插件 tar、发布者签名和包外 Provenance 继续向外闭合。
2. 首版执行一个有意收窄的 PEP 751 子集：单一默认安装集合、每个规范包名唯一，只允许本地 wheel；拒绝 `environments`、extras、dependency groups、VCS、directory、archive、sdist、editable 和运行时 URL。每个 wheel 必须位于 `supply-chain/python-wheels/`，并由锁记录文件名、大小和小写 SHA-256。
3. Manifest `requirements.python` 表示解释器范围；其他条目是插件直接依赖且必须使用精确版本。`pylock.toml` 保存完整传递闭包，两者必须一致。VastPlan Python SDK、gRPC 与 Protobuf 属于受信 Runtime Host 基座，不重复声明为每个业务插件的私有依赖。
4. 自动 SBOM 从 `pylock.toml` 读取完整包集合，不再从直接 requirements 推测。根组件同时记录 Python 范围和锁摘要；每个 PyPI 组件标记证据来自签名标准锁。
5. Artifact Trust 在仓库接收、读取和 Node 安装前重复执行有界 TOML 解析，验证锁声明、直接依赖、wheel 路径/大小/摘要，并拒绝锁未引用的额外 wheel。生产节点不联网、不重新解析版本，也不现场构建 sdist。
6. Node Agent 通过 `PythonDependencyInstaller` 窄接口把已验证 wheels 离线 materialize 到内容寻址安装目录下的 `.vastplan/python/site-packages`。默认 pip 适配器固定使用 `--no-index --no-deps --only-binary`；pip 只负责 wheel 安装，不取得解析或信任权。状态文件绑定锁摘要，未完成环境不会原子发布。
7. 独立 Python 进程通过可信启动环境挂载该 overlay；共享子解释器只把当前插件 overlay 加入对应解释器的 `sys.path`，并验证它位于已验签插件安装根下。一个物理 Host 可继续承载多个插件，但依赖可见性按执行单元隔离。

## 语言与运行方式

锁、Manifest、tar 和 wheel 的信任校验使用 Go，保持 Node Agent 与仓库的低依赖可信边界。解析与下载发生在构建环境，可使用 pip、uv 或其他能输出 PEP 751 的工具；节点默认使用 Python/pip 的离线 wheel 安装能力。`PythonDependencyInstaller` 保留替换为企业镜像安装器或其他 Provider 的空间，不改变插件制品契约。

## 取舍

wheel 随插件包发布会产生重复存储，但换来离线安装、不可变回滚和明确证据。后续可以在仓库内部按 wheel SHA-256 去重，不能把运行时联网重新引入安装路径。首版不接受按平台选择不同包版本；同一包版本可携带多个平台 wheel，由目标解释器选择兼容文件。若未来确有跨环境版本分歧，应增加多锁选择契约并先定义可信环境 marker 评估边界，不能静默放宽当前子集。

第三方 Python 插件仍受发布者隔离策略约束。完整锁解决可复现性，不把共享解释器变成安全沙箱。
