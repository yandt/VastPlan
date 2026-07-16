# VastPlan

基于 LLM 的通用 Agent 系统：在线 Agent 开发 + 远程客户端本地执行脚本/工作流 + 全栈插件机制。
采用**微内核 + 全层扩展点**架构，四内核（backend / frontend / runner / mobile），功能一律以第一方插件形式在骨架之上扩展。

> 设计文档从 **[docs/dev/00-index.md](docs/dev/00-index.md)** 进入（唯一索引）。

## 当前状态

**可运行的多节点骨架**——已跑通：全局部署规格 → 确定性多节点 assignment → Node Agent 自动装配 → 真实插件进程 → capability mesh → queue group 调用 → 健康恢复与实际态上报。

已实现：
- `proto/` 契约与协议单一真源（CallContext/CallTarget/CallResult/CallEvent + PluginHost 协议）
- `shared/go/registry` 扩展点注册表（分发语义、热装解绑、崩溃批量摘除）
- `shared/go/extpoint` 扩展点 descriptor 契约（宿主按它分发、插件按它声明）
- `schemas/plugin/v1` 插件清单、运行态 descriptor、制品元数据的 Draft 2020-12 Schema；
  协议注册与制品入库均 fail-closed 校验
- `schemas/deployment/v1` 单节点 DesiredState v1 Schema：service、固定单副本、启停、插件版本与节点标签
- `schemas/deployment/v2` 集群 Deployment v2 Schema：整数副本与精确标签放置；控制器展开为每节点 v1 assignment
- `shared/go/protocolbus` 协议总线宿主侧：**Channel 双向流**（宿主为服务端、插件回连）、
  **插件回调宿主**、心跳与崩溃探活、**select 权限判定**、**fanout 事件扇出**、
  **有序 Hook 分发**（before 可否决、after 只观察）
- `sdk/go/plugin` 插件 SDK（插件只写贡献 + 处理器）
- `kernels/backend` 后端内核骨架
- `kernels/backend/pluginservice` 本地及远端制品仓库：不可变索引、SHA-256、Ed25519 发布者证明、HTTPS 双重校验
- `shared/go/controlplane` NATS KV bucket、CAS 发布、节点/capability 租约；`shared/go/addressing` 本地直调与远端 NATS request-reply
- `kernels/backend/nodeagent` 自动装配：KV watch、内容寻址安装、真实健康与崩溃恢复、DRAIN、实际态复制、幂等与回滚
- `kernels/backend/deploymentcontroller` 多节点调度：rendezvous hashing、标签放置、持久 assignment generation 与节点漂移恢复
- Controller 按 Deployment 独立 CAS 选主并支持多副本接管；宿主 Drain 原子摘流并等待在途调用（ADR-0028）
- `plugins/` 四个第一方插件：hello-world（工具）、demo-permission（select 演示）、
  demo-audit（fanout 演示）、demo-quota（Hook 的配额限流与计量演示）

尚未实现（见文档待决）：资源/亲和调度、流式 RPC、持久事件、frontend/runner/mobile 内核。

## 快速开始

前置：Go 1.26.5+、protoc、`protoc-gen-go` 与 `protoc-gen-go-grpc`（`go install`）；集群演示另需启用 JetStream 的 `nats-server`。

```bash
# 1. 契约 codegen（改了 proto/ 后必跑）
./tools/gen-proto.sh

# 2. 构建（内核版本从 kernels/<name>/VERSION 经 ldflags 注入）
./tools/build.sh

# 2.1 把已构建二进制写入清单 entry.backend，并直接发布到本地仓库
go run ./tools/pluginpackage \
  -source plugins/com.vastplan.hello-world \
  -backend-bin bin/com.vastplan.hello-world \
  -repository .vastplan/repository

go run ./tools/pluginpackage \
  -source plugins/com.vastplan.demo-permission \
  -backend-bin bin/com.vastplan.demo-permission \
  -repository .vastplan/repository

# 2.2 启动单节点自动装配；修改 revision/plugins/enabled 后会自动对账
./bin/backend-kernel reconcile \
  -desired deploy/local.desired-state.json \
  -repository .vastplan/repository \
  -labels environment=local

# 2.3 本地开发集群模式（明文仅因显式 -nats-allow-insecure；生产见 NATS 安全指南）
# 终端 A：发布 v2 并持续运行调度控制器
go run ./tools/controlplane -nats-url nats://127.0.0.1:4222 -nats-allow-insecure -bootstrap \
  -desired deploy/cluster.deployment.json -controller

# 终端 B/C：两个节点使用独立运行目录；同 capability 自动进入同一 queue group
./bin/backend-kernel reconcile -nats-url nats://127.0.0.1:4222 -nats-allow-insecure \
  -deployment cluster-demo -tenant acme -node-id node-a -labels region=cn \
  -repository .vastplan/repository -runtime-root .vastplan/nodes/node-a/plugins \
  -actual-state .vastplan/nodes/node-a/actual.json
./bin/backend-kernel reconcile -nats-url nats://127.0.0.1:4222 -nats-allow-insecure \
  -deployment cluster-demo -tenant acme -node-id node-b -labels region=cn \
  -repository .vastplan/repository -runtime-root .vastplan/nodes/node-b/plugins \
  -actual-state .vastplan/nodes/node-b/actual.json

# 3. 跑 MVP 闭环：内核拉起三个插件并调用
#    权限插件必须装——没有它，所有调用被 fail-closed 拒绝（ADR-0021）
./bin/backend-kernel \
  ./bin/com.vastplan.demo-permission \
  ./bin/com.vastplan.demo-audit \
  ./bin/com.vastplan.hello-world

# 4. 测试
./tools/test.sh          # 单元 + 架构守护（快，日常）
./tools/test.sh --e2e    # 再加 E2E（跨进程真实链路）

# 5. 启用本地提交钩子（一次性；与 CI 同规则）
./tools/setup-hooks.sh
```

预期输出：内核版本 → 扩展点声明 → 三个插件回连/握手/**engines 校验**/贡献注册/激活 →
`greet`/`echo` 成功 → `whoami` **插件回调宿主**取内核信息 → 参数错误与未实现操作各自返回
**应用层错误**（与传输层错误区分）→ 未注册能力被拒 → **事件扇出**给审计插件（并可查账本验证
真送达）→ 未订阅类型无人接收 → 贡献摘除。

Hook 闭环由 `demo-quota` 与 E2E 覆盖：`invoke` 的 before Hook 按 priority 顺序执行、可返回
`hook.aborted` 否决调用；after Hook 仅接收已完成调用的结果并做计量，不能改写结论。

验证 fail-closed（不兼容内核应被拒绝装载）：

```bash
go build -ldflags "-X main.version=0.2.0" -o /tmp/k ./kernels/backend
/tmp/k ./bin/com.vastplan.hello-world   # → 内核 backend@0.2.0 不满足插件要求的 "^0.1"
```

## 工程门禁

CI（`.github/workflows/`）五道关：**lint**（gofmt/vet/staticcheck…）、**test + 架构守护**、
**E2E**、**codegen 与 proto 同步**、**security**（govulncheck 可达漏洞 + 依赖许可证白名单）。
PR 另检查提交规范与体量。

**架构守护**（`arch/`）把文档里的架构约束变成可执行断言——依赖方向、单一真源、布局纪律、文档死链，
违规即构建失败。规则见 [工程规范](docs/dev/guides/工程规范.md)。

## 版本机制

见 [ADR-0017](docs/dev/decisions/ADR-0017-版本定义与兼容性机制.md)。要点：内核/插件用 SemVer（真源分别是
`kernels/<name>/VERSION` 与 `vastplan.plugin.json#version`），协议用单调整数（握手取交集），
契约用包级 `vN`；兼容性在**五处 fail-closed 强制**（握手/激活/注册/调用/装配）。

## 代码布局

见 [docs/dev/guides/代码目录结构.md](docs/dev/guides/代码目录结构.md)。要点：**服务组合是配置不是代码**（backend/workspace/rs 是同一内核二进制 + 不同期望态），插件按 id 一目录、四面同处。
