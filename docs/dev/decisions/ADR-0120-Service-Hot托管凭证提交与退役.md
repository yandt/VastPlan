# ADR-0120 Service Hot 托管凭证提交与退役

- 状态：已采纳并实现
- 日期：2026-07-23
- 关联：[ADR-0090](ADR-0090-插件配置与托管凭证闭环.md)、[ADR-0114](ADR-0114-一次性ConfigurationAuthority与委托凭证暂存.md)、[ADR-0117](ADR-0117-语言中立Service-Hot配置控制器.md)、[ADR-0118](ADR-0118-独立配置资源与动态Profile.md)

## 背景

首个 `configuration.v1` 只开放无托管凭证 Service Hot。协调器能够暂存新秘密，但不掌握目标插件的完整 Active CredentialRef；只把本次新引用当成完整快照会丢失未重新输入的旧字段，把旧引用交给协调器又会扩大 handle 暴露面。

旧顺序“先把新凭证置为 Active，再提交目标配置”还存在不可补偿窗口：控制器 commit 失败时，新凭证已不能 abort，却没有任何 Active 配置引用它。相反，凭证插件允许 `Candidate` 和 `Active` 两种状态签发 Material Lease，这是为 readiness 和跨服务提交窗口预留的安全能力。

## 决策

### 1. Prepare 携带 replacement，不携带完整凭证快照

`configuration.v1 PrepareRequest.managedCredentials` 只包含本候选新暂存的字段。目标控制器是完整 Active 引用集的唯一所有者，必须执行：

1. 从私有 Active 状态读取旧引用；
2. 以本次 replacement 覆盖同字段，未出现的字段保持不变；
3. 校验合并结果满足插件业务约束，并可通过可信 Material Lease 探测 Candidate material；
4. 用 `values + 完整合并引用集` 计算 `configurationDigest`；
5. 把候选值、完整引用集和被替换旧引用的退役 outbox 一起耐久保存。

Go 契约提供 `MergeManagedCredentials/ReplacedManagedCredentials`，Node SDK 提供同义 helper。控制器 Observation 仍只返回摘要和状态，禁止返回引用。

动态数量的 OIDC/Webhook Profile 不把 `profiles.*` 秘密伪装成根配置固定字段，继续使用 ADR-0118 的独立资源集合；本 ADR 处理清单已声明的固定 Service Hot 托管字段。

### 2. 提交顺序改为“配置 commit，再完成凭证 Active”

协调 Saga 固定为：

```text
stage Delegated(Preparing)
  -> prepare Delegated(Candidate)
  -> controller.prepare(merge + probe + durable candidate)
  -> different-subject approval
  -> controller.commit(atomic Active switch)
  -> checkpoint FinalizingCredentials
  -> activate Delegated(Active)
  -> Ready
```

配置提交后、凭证激活前，目标运行时使用 Candidate 引用仍可取得受身份、用途、版本和 audience 约束的短时 Material Lease，因此不会出现“新配置已生效但秘密不可用”的半状态。若 commit 失败，新凭证仍是 Candidate，可继续恢复或在未提交时安全 abort，不会形成无人引用的 Active 版本。

控制器提交成功而协调器中断时，plugin-settings 以 `status` 的 Committed 事实进入或恢复 `FinalizingCredentials`，幂等重试 `activateDelegated`。不得依据本地超时猜测 commit 失败，也不得把 Controller Active 回写成第二份配置真相源。

### 3. 旧引用由目标控制器退役

只有目标控制器知道旧引用和真实 commit 事实，因此退役职责不能交给浏览器或 plugin-settings。控制器在原子 Active 切换时持久化私有 `retirePending`，提交后调用 `platform.credentials/retireManaged`，失败则在后续 `commit/status` 或自身恢复循环中幂等重试。

旧引用允许在 commit 后立即退役，因为完整新引用在 Candidate 状态已可安全 lease。公开 API 只投影 `configured/version/state`，不包含 handle、stage ID、authority、密文或 material。未替换的 Retained 字段在放弃候选时不得错误显示为 Aborted。

### 4. 语言与运行形态

协调器继续使用 Go：它已有持久 Saga、CAS、审计和可信宿主接口，改用 Node/Python 只会增加跨进程状态协调。目标控制器不绑定语言：

- Go 适合原生状态机、低资源常驻与强并发服务；
- Node 适合 OIDC、Webhook 和 HTTP 生态，可在共享 node-worker 中实现同一 wire；
- Python 适合数据/模型生态，但仍须在共享 Python Runtime 或隔离进程中实现相同幂等契约；
- Rust/Java/C# 等可在驱动生态更强时通过稳定 JSON/RPC 契约接入。

语言选择与进程形态分别决策；本能力不要求每个插件独占进程，也不要求与内核整体编译。

## 备选方案

- **协调器保存完整 Active CredentialRef**：形成第二真相源并扩大 handle 暴露，否决。
- **继续先激活凭证再 commit**：commit 失败产生不可 abort 的孤儿 Active 版本，否决。
- **commit 后必须等新凭证 Active 才允许运行**：忽略 Candidate Material Lease 的既有安全语义，并重新制造不可用窗口，否决。
- **把旧引用放进 Observation**：会让管理面、日志和多语言适配器获得不必要的敏感句柄，否决。
- **为动态 Profile 扩展通配字段 ID**：表单、授权和引用归属复杂且与独立资源协议重复，否决。

## 实施结果

- plugin-settings 0.11.0 开放带固定托管字段的 Service Hot Draft、留空保留、新值 replacement、异人审批、提交后凭证 finalization、重启恢复和安全版本投影。
- `FinalizingCredentials` 与 `configuration.hot-service.controller-committed` 审计检查点明确区分“控制器已提交、凭证元状态待完成”。
- Go/Node helper 与测试锁定合并和退役语义；端到端测试覆盖激活失败发生在 commit 之后、从 Committed 事实恢复、旧引用退役、Retained abort 不漂移以及公开响应不含 handle。
- Scoped Hot 托管凭证仍保持 fail-closed，留待其运行时引用投影和旧引用退役模型独立设计。
