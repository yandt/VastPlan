# VastPlan

基于 LLM 的通用 Agent 系统：在线 Agent 开发 + 远程客户端本地执行脚本/工作流 + 全栈插件机制。
采用**微内核 + 全层扩展点**架构，四内核（backend / frontend / runner / mobile），功能一律以第一方插件形式在骨架之上扩展。

> 设计文档从 **[docs/dev/00-index.md](docs/dev/00-index.md)** 进入（唯一索引）。

## 当前状态

**MVP 骨架**——已跑通最小闭环：内核声明扩展点 → 拉起插件进程 → 握手（magic + 版本协商 + 会话票据）→ 贡献注册进注册表 → 激活 → 按契约调用 → 摘除贡献。

已实现：
- `proto/` 契约与协议单一真源（CallContext/CallTarget/CallResult/CallEvent + PluginHost 协议）
- `shared/go/registry` 扩展点注册表（single/select/fanout/mount 分发语义、热装解绑）
- `shared/go/protocolbus` 协议总线宿主侧（进程拉起、握手、注册、调用、故障摘除）
- `sdk/go/plugin` 插件 SDK（插件只写贡献 + 处理器）
- `kernels/backend` 后端内核骨架
- `plugins/com.vastplan.hello-world` 验证插件

尚未实现（见文档待决）：Channel 双向流、节点代理 reconcile、内置插件服务、寻址层、NATS 控制面、frontend/runner/mobile 内核。

## 快速开始

前置：Go 1.24+、protoc、`protoc-gen-go` 与 `protoc-gen-go-grpc`（`go install`）。

```bash
# 1. 契约 codegen（改了 proto/ 后必跑）
./tools/gen-proto.sh

# 2. 构建（内核版本从 kernels/<name>/VERSION 经 ldflags 注入）
./tools/build.sh

# 3. 跑 MVP 闭环：内核拉起插件并调用它
./bin/backend-kernel ./bin/com.vastplan.hello-world

# 4. 测试
./tools/test.sh          # 单元测试（快，日常）
./tools/test.sh --e2e    # 单元 + E2E（跨进程真实链路）
```

预期输出：内核版本 → 扩展点声明 → 握手/协议协商 → **engines 校验** → 贡献注册/激活 →
`greet`/`echo` 调用成功 → 参数错误与未实现操作各自返回**应用层错误**（与传输层错误区分）
→ 未注册能力被拒 → 贡献摘除。

验证 fail-closed（不兼容内核应被拒绝装载）：

```bash
go build -ldflags "-X main.version=0.2.0" -o /tmp/k ./kernels/backend
/tmp/k ./bin/com.vastplan.hello-world   # → 内核 backend@0.2.0 不满足插件要求的 "^0.1"
```

## 版本机制

见 [ADR-0017](docs/dev/decisions/ADR-0017-版本定义与兼容性机制.md)。要点：内核/插件用 SemVer（真源分别是
`kernels/<name>/VERSION` 与 `vastplan.plugin.json#version`），协议用单调整数（握手取交集），
契约用包级 `vN`；兼容性在**五处 fail-closed 强制**（握手/激活/注册/调用/装配）。

## 代码布局

见 [docs/dev/guides/代码目录结构.md](docs/dev/guides/代码目录结构.md)。要点：**服务组合是配置不是代码**（backend/workspace/rs 是同一内核二进制 + 不同期望态），插件按 id 一目录、四面同处。
