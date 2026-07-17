# Backend 内核 1.0 封板指南

本文是 [ADR-0031](../decisions/ADR-0031-Backend内核1.0封板与工程门禁.md) 的活验收清单。它只记录 Backend Kernel 1.0 的当前证据和执行入口；架构原因以 ADR 为准，插件字段以《[插件契约与协议](../architecture/插件契约与协议.md)》为准。

## 1. 范围

纳入：Backend 插件宿主、Registry、协议/契约、寻址与事件、Node Agent、Deployment Controller、核心横切 SPI、发布和运行门禁。

排除：业务 Agent/Workflow/用户系统/LLM 实现，Frontend/Runner/Mobile 内核，以及具体权限、审计和可观测后端插件。

## 2. 验收矩阵

| 门禁 | 完成证据 | 当前状态 |
|---|---|---|
| 公共扩展点目录 | 7 个插件扩展点均有严格 manifest/runtime Schema；未知点和 `kernel.service` fail-closed | 已完成：Schema 枚举与内核常量有漂移守护，race/E2E 通过 |
| 契约兼容 | 兼容矩阵、稳定错误码、旧 SDK/插件夹具、破坏性变更门禁 | 已完成：可执行矩阵、错误码单一真源、proto 只增不改守护与独立 raw v1 客户端 E2E |
| 生命周期 | 显式状态机、幂等转换、升级迁移、失败回滚、实际态诊断 | 已完成：actual state v2、当前/候选双视图、检查点、copy-on-write 三阶段迁移、逆序 rollback 与真实进程 E2E |
| 协议资源边界 | payload/metadata/并发/队列/deadline/drain 有界且可配置 | 已完成：统一 `protocollimit`、每跳输入输出门禁、有界 dispatch/pending/NATS 队列、deadline 传播与 drain 时限，race/E2E 通过 |
| 多语言插件运行 | 语言无关驱动 SPI、Go/Python SDK、特性协商、发布者运行策略 | 已完成：native/python 驱动、双 SDK、真实跨语言 E2E；全局三态与发布者级优先覆盖，生产默认对未知发布者 fail-closed |
| 可观测与健康 | `slog`、trace、metric、health/readiness、诊断快照 | 已完成：JSON `slog` 出口、span 派生、可替换 metric sink、Host 健康/就绪与 `kernel.diagnostics` 无敏感快照 |
| 核心 SPI | 配置、凭证引用、persistence/transaction 边界可替换 | 已完成：`kernelspi.Dependencies`、unit 配置服务、凭证回调代理、强制 scope、事务冲突/回滚语义与会话插件身份注入 |
| 可靠性 | race、fuzz、故障注入、泄漏检查、24h soak | 主动延期：race、Schema fuzz、崩溃/迁移/断连故障 E2E、session/pending 收敛检查与 24h soak 工作流已就绪；待形成有代表性的插件与真实调用组合后重新冻结候选并执行，发布候选仍须附合格 24h 报告 |
| 性能 | 核心 benchmark 基线与 CI 回归阈值 | 已完成：注册/协议/本地寻址/调度/persistence 基准，PR 在同一 runner 比较 base/head；耗时 >50% 且 >100ns 或分配 >25% 阻断 |
| 安全与供应链 | mTLS/NKey/ACL、漏洞/许可证、签名制品、SBOM | 已完成：运行链路、发布者签名、漏洞/许可证 CI、逐目标 CycloneDX SBOM 与 OIDC 来源/SBOM 证明均有机器门禁 |
| 发布运维 | 可复现构建、版本升级、回滚、配置迁移、诊断 runbook | 已完成：逐字节复现门禁、内置 version/validate/support-bundle、tag/version 守卫及可执行升级回滚手册 |
| 代码质量 | 分层依赖、单一类型真源、重复度、生产函数复杂度、Shell 脚本 | 已完成：Backend 横向依赖/生产入口/DTO 架构守护，dupl/gocyclo/ShellCheck CI 与安全工具精确版本 |

只有全部门禁都有当前仓库或运行记录证据，才允许把 `kernels/backend/VERSION` 更新为 `1.0.0`。

## 3. 日常验证

```bash
./tools/test.sh
./tools/test.sh --e2e
go test -race ./...
PYTHONPATH=sdk/python python3 -m unittest discover -s sdk/python/tests -v
go test -tags=e2e ./e2e -run TestPythonPlugin_CrossLanguageInvokeAndFeatureNegotiation
go vet ./...
./tools/benchmark.sh
./tools/benchmark.sh --compare <base-ref>
go test -run='^$' -fuzz=FuzzParseManifest -fuzztime=30s ./schemas/plugin/v1
./tools/verify-release.sh
```

发布候选的 24 小时稳定性记录由 GitHub Actions `Backend Kernel Soak` 手工工作流产生，输入必须保持 `24h`。报告检查真实插件调用和周期重启，并验证 goroutine、文件句柄、session pending 不持续增长；短时 smoke 只能验证入口，不能替代发布证据。

2026-07-16 决定暂缓正式 soak：当前只有单一 `legacy-v1/echo` 合成链路，尚不足以代表后续插件生态的混合负载。已取消提交 `0c128a7692da48845c70b2d6472b013a50bac37b` 对应的 run `29480318827`；该运行及其任何部分结果不得作为发布证据。恢复条件是至少具备可代表实际业务路径的多类插件与负载模型，届时必须以新的冻结提交重新运行完整 24 小时，不得续跑或复用本次记录。

Release 只接受 `kernels/backend/SOAKED_COMMIT` 指向提交的合格报告，并通过 `tools/soakreport` 复验。被测提交之后若出现任何非版本推广白名单改动，必须对新的冻结提交重新运行 24 小时 soak。

封板新增的兼容、fuzz、benchmark、故障和 soak 入口落地后，继续登记在本节；不得把只存在个人命令历史里的验证当作发布证据。

## 4. 提交纪律

每个门禁按“实现 + 测试 + 权威文档”形成独立稳定提交。涉及公共契约的提交必须说明兼容影响；破坏性变化必须新增 ADR，不得覆盖既有决策。
