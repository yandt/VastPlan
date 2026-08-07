# ADR-0196 四内核规范 ID 由 runner 改名为 desktop

- 状态：已采纳
- 日期：2026-08-07
- 关联：[ADR-0014 四内核结构](ADR-0014-四内核结构.md)、[ADR-0015 内核与贡献面命名规范](ADR-0015-内核与贡献面命名规范.md)、[ADR-0012 APP 内核运行模型](ADR-0012-APP内核运行模型.md)、[ADR-0013 APP 多档能力与手机 Companion](ADR-0013-APP多档能力与手机Companion.md)、[ADR-0106 多端统一身份授权与执行租约](ADR-0106-多端统一身份授权与Runner执行租约.md)

## 背景

ADR-0014 拆出第三个内核时命名为 `runner`，取"执行器"之意。实际推进中该名字有两个问题：

1. **语义过宽**：`runner` 在工程语境里普遍指"任务执行器"——CI runner、test runner、task runner、Node Worker 里的 runner。仓库内同时存在 `core/runtimehosts/node-worker/runner.mjs`（Worker 任务执行器）和 `engineering/tools/benchcompare`（注释里的 CI runner），与内核名同形不同义，读代码和检索时需要人工分辨。
2. **未表达档位差异**：ADR-0013 的核心区分是"桌面端跑完整本地执行能力，手机端只做交互与轻任务"。`runner` 不携带"桌面"这一关键信息，而 `mobile` 携带了平台信息，两者不对称。

改名为 `desktop` 后，四内核规范 ID 分别按"服务端 / 浏览器 / 桌面 / 手机"的运行环境命名，语义齐整，且与既有的 `mobile` 对称。

曾考虑过 `client`，但会与 `core/shared/go/clientcore`（ADR-0014 规划的 desktop + mobile 共享层）产生歧义：desktop 与 mobile **都**是 client，用 client 专指桌面端会让 `clientcore` 的范围读不清。`desktop` 不存在这个问题，`clientcore` 的语义反而更准确。

## 决策

**规范 ID `runner` → `desktop`，贯穿代码、契约、配置、清单贡献面与文档。**

| 显示名 | 规范 ID | 贡献面 | 目录 | 运行环境 |
|---|---|---|---|---|
| Backend 内核 | `backend` | `contributes.backend` | `core/kernels/backend/` | 服务端 |
| Frontend 内核 | `frontend` | `contributes.frontend` | `core/kernels/frontend/` | 浏览器 |
| **Desktop 内核** | **`desktop`** | **`contributes.desktop`** | **`core/kernels/desktop/`** | 桌面/服务器客户端执行器 |
| Mobile 内核 | `mobile` | `contributes.mobile` | `core/kernels/mobile/` | 手机 iOS/Android |

具体范围：

1. **目录**：`core/kernels/runner/` → `core/kernels/desktop/`（`git mv`，保留历史）。
2. **契约常量与枚举**：`KernelRunner`→`KernelDesktop`、`PluginTargetRunner`→`PluginTargetDesktop`、`RunnerCapability`→`DesktopCapability`（值 `runner.capability`→`desktop.capability`）、`SurfaceRunnerLocal`（`runner.local`→`desktop.local`）、`PurposeRunnerToken`（`runner-token`→`desktop-token`）、`OwnerRunnerInstall`（`runner-install`→`desktop-install`）。
3. **JSON Schema**：`engines.runner`→`engines.desktop`、`entry.runner`→`entry.desktop`、`contributes.runner`→`contributes.desktop`，`$defs` 中 `runnerContributes`/`runnerCapability`/`runnerCapabilities`/`runnerInteractionContribution` 一并改名；`kernel`/`target`/`surface` 枚举值同步。
4. **Go/TypeScript 标识符**：`RunnerAdapter`→`DesktopAdapter`、`ApplyRunnerProfile`→`ApplyDesktopProfile`、`RunnerIdentity`→`DesktopIdentity`、`SupportsRunner`→`SupportsDesktop`，TS 联合类型 `"runner"`→`"desktop"`。
5. **v1 Schema 原地修改，不开 v2**：改名时 `contributes.runner` 的实际使用者为 **0 个插件**，只有 Schema 定义与测试夹具引用；`runner-token`/`runner-install` 的使用者是首方 oidc provider 与制品仓库 GC，同批改完。系统仍为 `0.1.0`，Desktop 内核尚未有已发布制品，因此不构成对外兼容性破坏。

**保留不改的项（假阳性）**：`core/runtimehosts/node-worker/runner.mjs`（Worker 任务执行器）、benchcompare 注释与 CI `runs-on` 中的 runner、指南里的 "task runner" 表述。这些是"任务执行器"通义，与内核名无关。

**ADR-0106 文件名保留 `Runner` 字样**：ADR 只追加不覆盖，改文件名会破坏既有引用。文件名与标题中的历史用词保留，正文含义以本 ADR 为准。

## 影响

- 正面：四内核规范 ID 按运行环境齐整命名，`desktop`/`mobile` 对称；内核名与"任务执行器"通义不再同形；`clientcore` 作为 desktop+mobile 共享层的语义变清晰。
- **已知遗留不一致**：wire 契约 `contract.proto` 的 `CALLER_KIND_RUNNER = 5` **未改**。三个原因叠加：该文件明确标注"不可变契约"；`engineering/arch/compatibility_test.go` 把该枚举冻结进 V1 兼容矩阵；本地 protoc 与 CI 官方构建对 Python `pb2` 序列化描述符的转义表示不同（字节相同、文本表示不同），本地无法产出与 CI 一致的生成物，提交任何本地生成物都会让 `codegen` job 出现反向 diff。因此当前状态是"内核叫 `desktop`，caller kind 仍叫 `CALLER_KIND_RUNNER`"。
- 消除该遗留的正确做法是 protobuf 的加法式演进：新增 `CALLER_KIND_DESKTOP = 6` 并弃用 `= 5`，而非原地改名；这需要单独 ADR 与能复现 CI 生成物的环境，不在本 ADR 范围。
- 需同步：已完成 140 个文件的代码/契约/文档改名，Go 构建、全量 Go 测试、架构门禁（含文档死链）、前端 typecheck 与前端测试均通过。
