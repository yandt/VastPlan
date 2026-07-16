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
| 生命周期 | 显式状态机、幂等转换、升级迁移、失败回滚、实际态诊断 | 未完成 |
| 协议资源边界 | payload/metadata/并发/队列/deadline/drain 有界且可配置 | 未完成 |
| 可观测与健康 | `slog`、trace、metric、health/readiness、诊断快照 | 未完成 |
| 核心 SPI | 配置、凭证引用、persistence/transaction 边界可替换 | 未完成 |
| 可靠性 | race、fuzz、故障注入、泄漏检查、24h soak | 部分完成：race/E2E 已有 |
| 性能 | 核心 benchmark 基线与 CI 回归阈值 | 未完成 |
| 安全与供应链 | mTLS/NKey/ACL、漏洞/许可证、签名制品、SBOM | 部分完成：运行链路和签名制品已有 |
| 发布运维 | 可复现构建、版本升级、回滚、配置迁移、诊断 runbook | 未完成 |

只有全部门禁都有当前仓库或运行记录证据，才允许把 `kernels/backend/VERSION` 更新为 `1.0.0`。

## 3. 日常验证

```bash
./tools/test.sh
./tools/test.sh --e2e
go test -race ./...
go vet ./...
```

封板新增的兼容、fuzz、benchmark、故障和 soak 入口落地后，继续登记在本节；不得把只存在个人命令历史里的验证当作发布证据。

## 4. 提交纪律

每个门禁按“实现 + 测试 + 权威文档”形成独立稳定提交。涉及公共契约的提交必须说明兼容影响；破坏性变化必须新增 ADR，不得覆盖既有决策。
