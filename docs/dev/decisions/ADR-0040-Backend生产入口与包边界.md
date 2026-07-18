# ADR-0040 Backend 生产入口与包边界

- 状态：已采纳
- 日期：2026-07-16
- 关联：[ADR-0016 单仓与代码目录布局](ADR-0016-单仓与代码目录布局.md)、[ADR-0020 代码设计原则与复用策略](ADR-0020-代码设计原则与复用策略.md)、[ADR-0038 Backend 可复现发布与运维交付](ADR-0038-Backend可复现发布与运维交付.md)

## 背景

控制面 Controller 和远端制品仓库已经是生产运行组件，却以 `engineering/tools/controlplane`、`engineering/tools/artifactserver` 两个额外 `main` 包存在。这与“`engineering/tools/` 只放 codegen、构建、测试、发布工具”和“Backend 服务由同一内核二进制组合”的规则冲突，也使发布流程没有交付这些入口。

同时，`nodeagent` 的消费接口直接引用同级 `pluginservice.Ref/Artifact`，让消费者依赖生产者实现包；未来替换仓库实现会把实现依赖扩散到对账核心。

## 决策

1. Controller 和制品仓库成为 Backend 二进制的 `controlplane`、`artifact-server` 子命令。实现位于 `core/kernels/backend/commands/`，这里是允许装配同级内核组件的组合根。
2. `engineering/tools/` 不允许保存生产服务入口；发布包仍只交付一个 Backend 内核二进制。
3. 跨 Backend 子包的稳定制品 DTO 定义在 `contracts/schemas/plugin/v1`。`nodeagent` 接口只依赖 `ArtifactRef/Artifact`；`pluginservice` 保留别名以维持源代码兼容。
4. 长运行子命令接收 context 并执行优雅关闭；参数错误和 `-help` 不调用 `os.Exit`，便于内核统一处理与单元测试。

## 备选方案

- **发布三个独立二进制**：进程职责直观，但扩大版本、SBOM、部署和运维矩阵，并违背当前单 Backend 内核组合决策。拒绝。
- **保留 `engineering/tools/` 入口但补充发布构建**：能交付，却继续模糊开发工具与生产运行时代码的边界。拒绝。
- **把制品 DTO 放 `core/shared/go`**：可解耦，但 DTO 已属于插件 v1 Schema 契约，另建位置会形成第二真源。拒绝。
- **由 nodeagent 自己定义输入类型并做适配**：消费者所有权更纯，但会重复 schema 已定义字段并增加转换层。拒绝。

## 影响

- 正面：生产入口、版本、发布证明和运维命令收敛到一个二进制；`nodeagent` 不再依赖仓库实现包。
- 代价：原 `go run ./engineering/tools/...` 命令需要迁移到 `go run ./core/kernels/backend <subcommand>`；命令组合包是显式允许的同级依赖汇聚点。
- 后续：架构门禁应禁止 `engineering/tools/` 下出现生产入口，并限制 Backend 普通子包之间的横向依赖。
