# 平台控制数据库 Bootstrap

本文是 `PlatformControlStoreProfile`、Bootstrap Secret Provider、两阶段状态机和 `state.shared.v1` 运行期绑定的实现真源。数据分层决策见 [ADR-0194](../decisions/ADR-0194-平台控制数据库与声明式数据分层.md)，恢复分级见 [ADR-0169](../decisions/ADR-0169-Seed-Recovery-Capsule与分阶段可用性.md)。

## 1. 本机只保存非敏感根

`contracts/schemas/platformcontrol/v1` 定义严格 Profile：generation、PostgreSQL/MySQL Provider、`host:port`、逻辑 database/schema、TLS、username、secretRef 和 Database Runtime capability range。Profile 不允许 DSN、URL、密码或任意驱动参数。

Profile 文件必须使用规范绝对路径、普通文件和 owner-only 权限。提交采用 `expected generation -> candidate generation=expected+1` 的 CAS、临时文件、fsync 和原子 rename；初始化成功但 Profile CAS 失败时不得绑定候选 Store，调用方可按同一幂等初始化流程重试。

## 2. 秘密只在回调期间存在

组合根根据 Profile 的 `secretRef` 选择一次 `SecretSource`：

- Linux 生产使用 `systemd-credential`，只允许安全 credential name，并从规范 `CREDENTIALS_DIRECTORY` 读取；
- 开发使用 `owner-file`，要求规范绝对路径、普通文件和无 group/world 权限；
- Vault/KMS 后续通过同一 `SecretSource` 接口增加，不复制 Bootstrap 流程。

秘密最大 64 KiB，只借给同步回调，回调结束立即清零缓冲区。Profile、状态、日志和错误码不包含秘密或原始数据库错误。

## 3. 两阶段状态机

可信 Controller 只依赖 `ProfileStore`、`SecretResolver`、`Bootstrapper` 和 `BindingStore` 四个端口：

```text
Start
  ├─ 无 Profile → unconfigured
  └─ 有 Profile → Open → Bind → ready

Configure
  Test → Initialize → Profile CAS Commit → Bind → ready
```

状态只有 `unconfigured / testing / initializing / ready / recovery` 和稳定错误码。首次配置任一步失败都进入最小 recovery 且 Shared State 保持 unavailable；已有 Ready generation 的替换候选失败时保留旧 Store 和旧 generation，只投影候选错误码。Profile 已提交但绑定异常属于不应发生的信任边界故障，必须 recovery，不能继续把旧 Store 冒充当前 Profile。

## 4. 不可回退 Shared State 绑定

`sharedstate.BindingStore` 是 Kernel 暴露给插件的稳定 `state.shared.v1` 端口。它启动时 unavailable，只接受更高 generation 的 Provider；同 generation 只有相同 Profile identity 才幂等。Provider 故障不会切回本地 JSON，避免同一插件出现 SQL 与文件双真相源。

File Provider 仍只服务单测、开发未初始化阶段和明确的 Seed/Recovery 根状态，不能绑定为生产 Platform Control Store。P4 后续检查点将把 Database Runtime 的 `foundation.state.shared.sql` 适配到 `Bootstrapper/BindingStore`，再让 Node Agent 按 Bootstrap Tier 与 Full Tier 分阶段装配。

## 5. 当前状态

P4a 已完成 Profile 契约、owner-only CAS 文件存储、systemd/development file Secret Source、两阶段 Controller 和不可回退 Binding Store，并覆盖空环境、首次成功、失败不提交、旧代保留、权限与秘密清零测试。Database Runtime 远端适配、Bootstrap Tier 启动、配置 API/UI 和恢复动作仍属于 P4 后续。
