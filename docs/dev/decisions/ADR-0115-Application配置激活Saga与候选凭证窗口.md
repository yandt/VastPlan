# ADR-0115 Application 配置激活 Saga 与候选凭证窗口

- 状态：已采纳
- 日期：2026-07-23
- 关联：[ADR-0090](ADR-0090-插件配置与托管凭证闭环.md)、[ADR-0113](ADR-0113-可信插件配置目录与分路径生效.md)、[ADR-0114](ADR-0114-一次性ConfigurationAuthority与委托凭证暂存.md)

## 背景

Application 来源的 `service + restart` 配置不能由 plugin-settings 直接改 Deployment，也不能复用普通服务发布 API 绕过审批和 readiness。带秘密候选还有一个顺序矛盾：若凭证在发布前已成为长期 Active，失败候选会留下可用秘密；若等服务 Ready 后才允许读取，依赖秘密启动的候选进程永远无法 Ready。

## 决策

### 1. 共享 Saga 契约，Deployment Manager 持有外部副作用

`core/shared/go/configurationactivation` 定义不含秘密的候选协议。plugin-settings 只提交候选 ID、不透明配置 ID、旧 Catalog/Schema/Artifact 摘要、非敏感 values 和新托管引用；Deployment Manager 必须从自己持有的当前活动 ServiceRevision 与 ConfigurationCatalog 重新派生 deployment、unit、plugin、来源和最终配置，不信任调用方提供这些字段。

创建操作以 candidate ID 与完整请求摘要联合幂等，直接生成 `PendingApproval` 服务修订并记录提交人；相同 ID 携带不同 values 或凭证引用必须冲突失败。普通 `publishServiceRevision` 拒绝候选绑定修订；只有专用配置激活操作可在审批后发布、观察精确 revision readiness，并在失败时从上一活动修订创建新的单调 rollback revision。

### 2. 配置候选不复制 Deployment 状态机

plugin-settings 的状态与外部状态分别记录：

```text
Draft --submit--> Publishing(PendingApproval/Approved)
                       |
                       +--activate--> Activating(Publishing)
                                             |
                              Ready <--------+--------> RolledBack|Failed
```

提交和激活均使用候选 revision CAS。审批继续由 Deployment Manager 和 `platform.deployment.approve` 的不同主体完成；发布入口要求 `platform.plugin-configuration.publish`。协调器只通过三个精确内部操作创建、查询和发布候选绑定修订，不能列举或编辑其他服务修订。

### 3. 委托凭证增加内部 Candidate 窗口

委托凭证状态增加内部 `Candidate`：

```text
Preparing -> Candidate -> Active
                  |
                  +------> Aborted
```

进入实际 Deployment 发布前，配置协调器把该候选的新凭证从 Preparing 推进到 Candidate。Candidate 与 Active 都可向可信宿主签发 material lease，但不可猜 handle 只存在于候选 Deployment 的 `managed_credentials.<plugin>.<field>` 投影；旧活动实例、其他插件、浏览器和普通配置值均得不到它。readiness 失败时回滚 Deployment 并把 Candidate 凭证 Aborted；readiness 成功后再将其提交为长期 Active。

material lease 在解密后再次检查状态，Abort/Retire 竞态优先。协调器或凭证插件重启后，prepare/activate/abort 均按 candidate ID 幂等恢复。

### 4. 托管引用使用独立宿主投影

ServiceUnit 配置信封增加 `managed_credentials`，与普通 `plugins` values 分离。Node Agent 将其冻结为按认证插件隔离的 provider；目标插件只能通过 `kernel.config.credential-ref/get(fieldId)` 读取自己的引用。启动环境变量和 `kernel.config.get` 仍只包含非敏感 values，避免给既有配置结构注入保留字段。

ConfigurationCatalog 只公开字段是否已配置和版本，不公开 handle。编辑已配置的必填秘密时，空输入表示保留当前引用；新输入只覆盖对应字段。

## 备选方案

- **发布前直接 Active**：失败候选会扩大长期可用秘密窗口，否决。
- **Ready 后才允许 material lease**：依赖秘密启动的候选形成死锁，否决。
- **把 handle 混入普通 values**：破坏签名 Schema、增加日志和 UI 泄漏面，否决。
- **允许普通服务发布入口处理配置修订**：可绕过候选凭证准备、readiness 补偿和配置审计，否决。
- **由 plugin-settings 自行写 Application Composition/KV**：越过 Deployment Manager 和可信内核边界，否决。

## 影响

- Application 来源的 restart 配置已具备 Draft、独立审批、发布、readiness、凭证提交和单调回滚闭环。
- Platform Profile 与 hot 配置仍复用相同候选状态和凭证窗口，但各自由后续适配器提供真实 Active 真源。
- 目标插件若声明通用 `managedCredentials`，必须通过宿主引用端口取得 CredentialRef，再使用受控宿主执行能力消费 material；插件本身仍不能解密。
