# ADR-0175 统一版本生命周期与 Resource Adapter 能力协商

- 状态：已采纳，P2.2.2 待实施
- 日期：2026-07-31
- 关联：[ADR-0172](ADR-0172-通用版本账本与可插拔存储Provider.md)、[ADR-0173](ADR-0173-版本环境与资源适配.md)、[ADR-0174](ADR-0174-Portal可选版本控制与发布快照分离.md)

## 背景

Portal 只是第一个 Version Workspace 消费者。未来系统设置、数据库定义、工作流、脚本目录、文本、图片、PDF、模型和其他二进制资源都可能需要版本管理。如果每种资源各自定义 Session、Lease、CAS、commit 和历史接口，版本身份、幂等、安全和多语言 SDK 会迅速漂移。

反过来，要求所有资源实现完全相同的 diff、merge、预览和物化也不成立：JSON 可以返回字段路径，文本适合行级 diff，目录适合路径 diff，图片或压缩包通常只能可靠判断摘要变化。伪造空 diff 或把二进制塞进 JSON/Ledger 都会造成错误语义和性能风险。

## 决策

1. 所有启用版本控制的领域统一使用 `version.workspace.v1` 的 Session、Lease、revision CAS、operationId、commit/discard、VersionRef 和已提交版本读取语义，不为文本、文件或二进制另建生命周期协议。
2. Resource Adapter 使用同一 `version.resource.v1` SPI。`describe` 与 `normalize/validate` 是强制能力；`diff`、`materialize` 和 `merge` 是显式可选能力，调用方必须能力协商，不能依靠调用失败猜测。
3. Workspace 增加 `describeResource`，按受信 Environment Binding 返回内容形态、允许模式、大小上限和语义能力。结果不暴露 Adapter 配置、Provider、endpoint、对象路径或凭证。
4. `changes` 永远可以通过规范 Snapshot digest 判断 `dirty`。Adapter 不支持 diff 时返回 `diffAvailable=false`、空路径和零统计；不得把“内容已变化但无语义 diff”伪装成 clean。
5. `compareCommitted` 遵循相同结果模型。两端摘要是否不同始终可判定；只有 Adapter 声明 diff 时才返回路径与统计。对未支持的 merge/materialize 调用返回稳定 `operation_unsupported`。
6. 底层内容形态保持最小的 `json / files` 两类，不为每种 MIME 新增 Wire union：
   - 小型结构化对象使用规范 JSON，受 Ledger 1 MiB 上限约束；
   - 任意文本、单个二进制和目录都使用内容寻址 Files Manifest，实际字节进入受信 Object/Artifact Store，Ledger 只保存有界清单。
7. 标准 Text、Blob、Files Adapter 可以共享 `ContentFiles`，但拥有不同语义：Text 校验编码并提供行级 diff；Blob 校验媒体、大小和扫描结论，通常无内容 diff；Files 校验路径树并提供路径 diff/懒物化。
8. 业务插件只绑定 ResourceType 到已有 Adapter。只有新资源需要领域规范化、特殊安全规则或语义 diff 时才实现新 Adapter；消费版本功能不等于每个插件都实现 Adapter。
9. 文件媒体类型和扩展名不是信任依据。Adapter/对象存储必须复核摘要、大小、允许类型、压缩炸弹、路径穿越、符号链接和恶意内容；秘密仍只能保存 CredentialRef，不能因使用 Files Manifest 绕过秘密边界。
10. Go、Python、Node 和未来其他语言 SDK 共享同一 Workspace 操作和 capability 字段。语言实现不得发明同义操作或用本地框架类型污染 Wire 契约。
11. 大文件字节不通过 JSON Capability Bus。P2.4 建立统一 Content Staging：可信宿主签发有界上传 Lease，调用方把字节流写入内容寻址 Store，Store 复核 digest/size/tenant/安全结论后，Workspace 才接受引用这些对象的 Files Manifest。上传 Lease、对象物理路径和存储凭据都不进入版本记录。

## 内容映射

| 资源 | Adapter | Wire 内容 | 详细 diff |
|---|---|---|---|
| 配置、规则、Schema | JSON/领域 Adapter | `json` | 字段/语义路径，可选 |
| UTF-8 文本、脚本、模板 | Text Adapter | 单项 `files` 清单 | 行级，可选 |
| 图片、PDF、模型、压缩包 | Blob Adapter | 单项 `files` 清单 | 通常不支持 |
| 目录、项目、资源包 | Files Adapter | 排序 `files` 清单 | 路径级，可选 |
| 超大数据集 | 领域 Adapter + 外部不可变引用 | JSON/清单引用 | 由领域决定 |

## 备选方案

- **所有类型实现完全相同功能**：会产生假的 binary diff/merge，拒绝。
- **每种类型独立 Workspace 协议**：重复 Session、Lease、CAS 和 SDK，拒绝。
- **为 text/blob/image/pdf 增加多个 Snapshot union**：媒体类型组合无限增长，而 Files Manifest 已能无损表达，拒绝。
- **把任意字节直接写进 Ledger**：破坏有界 JSON、数据库效率、安全扫描和对象去重边界，拒绝。

## 影响

正面：所有插件获得一致版本生命周期；新资源通常只需配置绑定；任意文件类型可安全版本化；UI 能按能力隐藏不适用操作；多语言 SDK 不分叉。

代价：P2.2.2 需要调整 ChangesResult、Adapter 接口和能力发现；Text/Blob/Files 还需要 P2.4 Content Staging、对象存储端口、安全准入和各自契约测试，不能只创建空清单插件。
