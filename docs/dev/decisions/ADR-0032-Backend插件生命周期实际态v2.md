# ADR-0032 Backend 插件生命周期与实际态 v2

- 状态：已采纳
- 日期：2026-07-16
- 关联：[ADR-0024 单节点自动装配与回滚语义](ADR-0024-单节点自动装配与回滚语义.md)、[ADR-0028 控制器选主与 Drain 收敛](ADR-0028-控制器选主与Drain收敛.md)、[ADR-0031 Backend 内核 1.0 封板与工程门禁](ADR-0031-Backend内核1.0封板与工程门禁.md)

## 背景

Node Agent 原有实际态只在一次 reconcile 末尾写入 `running/degraded/stopped` 自由字符串。它能回答“最后一次结果是什么”，但无法区分当前稳定实例失效和升级候选失败，也看不到安装、激活、排空、停用等中间阶段。控制面因而不能可靠诊断卡住的操作，状态拼写也没有机器可验证的封闭集合。

升级路径已经具备“候选先启动、成功后替换、失败保留旧实例”的运行事实。实际态必须忠实表达这条路径，不能为了报告候选失败而覆盖仍在服务的旧版本。

## 决策

### 1. 实际态升级为 v2

`ActualState.version` 升为 `2`，`UnitState.status` 替换为封闭类型 `phase`。允许状态固定为：

- `uninstalled`
- `installed_inactive`
- `activating`
- `active`
- `draining`
- `deactivating`
- `failed`
- `removed`

每个状态记录 `phase_changed_at`。转换图由代码中的单一目录校验，未知状态和未声明跳转 fail-closed；同状态转换幂等。

不存在于 `units` 表示稳定的未安装事实。执行安装时会先写入 `uninstalled` 检查点；删除记录前会短暂写入 `removed`。NATS actual-state KV 每个节点有界保留最近 16 个检查点，既支持事后诊断，也不把状态存储变成无限审计日志。

### 2. 当前实例与候选实例分离

`UnitState` 表示当前稳定实例，`candidate` 表示尚未获得所有权的候选组合。首次安装时顶层 phase 与候选同步；升级期间顶层继续报告旧实例的真实状态，候选独立经过：

`uninstalled -> installed_inactive -> activating -> active|failed`

候选启动成功后一次性替换当前实例并清除 `candidate`。下载、安装或启动失败时，候选保留 `failed` 和错误原因；旧实例的 fingerprint、进程和 `active` 状态不被覆盖。

### 3. 中间状态必须持久化

Reconciler 在下载、激活、排空、停用等有副作用的长操作前后写检查点。检查点持久化失败时，不继续执行后续副作用。最终 reconcile 仍写入观察 revision、应用 revision 和操作错误，检查点不能替代最终收敛判定。

### 4. 兼容读取旧实际态

文件和 NATS 状态存储共享同一解码入口。读取 v1 时将：

- `running` 映射为 `active`
- `stopped` 映射为 `installed_inactive`
- `degraded` 映射为 `failed`

迁移只发生在读取边界，后续写回只产生 v2；未知 v1 status 或未知版本拒绝加载。该迁移是 Node Agent 自身诊断状态的格式迁移，不等同于插件业务状态迁移。

### 5. 运行事实变化才复制到控制面

本地实际态文件仍在每个检查点更新 `updated_at`，保证节点恢复诊断完整；同步到 NATS 的副本则忽略该纯时间字段。若 version、revision、unit 生命周期、readiness、候选、错误或进程事实均未变化，不产生新的 KV revision。节点存活继续由 Node Lease 续租表达，不把 ActualState 当心跳。

Actual KV key 固定为 `tenant/deployment/actual/node` 作用域，与节点租约和 assignment 一致。同一物理节点为多个部署运行 Node Agent 时，实际态互不覆盖；Controller 只消费自身 deployment 前缀的 Node/Actual Watch 事件。

开发与运维启动门禁不得仅按持久 ActualState 中的 `active/ready` 数量判定成功。门禁必须同时确认该快照由本次 Node Agent 启动后重新写入，并且每个目标 unit 的 `candidate` 为空；否则上次运行的 Ready 或“旧实例仍服务、候选仍启动”的中间态会产生假就绪。

## 备选方案

- **保留 `status` 并新增更多字符串**：改动较小，但无法阻止拼写漂移，也没有明确转换图。拒绝。
- **升级失败直接把 UnitState 标成 failed**：字段更少，却会错误表达仍在服务的旧实例。拒绝。
- **只在 reconcile 末尾保存最终状态**：写入次数少，但控制面仍无法诊断卡在安装、激活或排空的操作。拒绝。
- **把每次转换永久追加为事件日志**：审计能力更强，但需要独立留存、压缩和查询策略；1.0 先以状态检查点和 NATS KV 历史封板，完整审计日志由事件出口提供。

## 影响

- 正面：生命周期成为可验证状态机，升级候选与当前实例事实不再混淆，控制面可以诊断中间阶段。
- 代价：长操作仍会产生更多本地检查点；观察方必须识别 actual state v2。无运行事实变化的周期对账不再放大为控制面写入或全局重算。
- 兼容：v1 本地/NATS 实际态可自动读取；实际态是节点内部控制面协议，不改变插件 v1 wire 协议。
- 后续：有状态插件的业务状态版本、迁移事务和回滚契约仍按 ADR-0031 单独落地，不能以本 ADR 的 actual state 迁移冒充完成。
