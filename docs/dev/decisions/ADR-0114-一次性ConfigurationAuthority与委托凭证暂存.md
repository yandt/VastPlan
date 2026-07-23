# ADR-0114 一次性 ConfigurationAuthority 与委托凭证暂存

- 状态：已采纳
- 日期：2026-07-23
- 关联：[ADR-0090](ADR-0090-插件配置与托管凭证闭环.md)、[ADR-0092](ADR-0092-业务插件拥有托管凭证生命周期.md)、[ADR-0113](ADR-0113-可信插件配置目录与分路径生效.md)

## 背景

通用配置协调器需要把用户在目标插件配置页输入的秘密交给凭证托管器，但它不是秘密最终 owner。继续调用普通 `stageManaged` 会把 owner 绑定成协调器；允许浏览器或协调器提交 owner/purpose 又会形成可伪造的高权限秘密代理。

配置协调器、凭证托管器和目标插件还可能位于不同 Backend 服务。授权既要跨服务可验证，又必须短时、一次性、可审计并能在多内核并发消费时防重放。

## 决策

### 1. 宿主按活动可信目录派生授权

只有认证后的 `cn.vastplan.platform.configuration.plugin-settings` 能调用 `kernel.configuration.authority.issue`。请求只包含不透明配置 ID、活动目录摘要、候选 ID 和托管字段 ID。可信宿主重新读取当前 `ConfigurationCatalog v1`，从中派生且锁定：

- tenant、deployment、unit 和配置资源；
- 目标插件 owner；
- 字段 ID 与 purpose；
- 精确制品 SHA-256 和 Schema digest；
- 配置候选 ID、签发时间和过期时间。

协调器、浏览器和凭证插件都不能覆盖这些事实。每个字段使用独立授权，默认有效期 45 秒，上限 60 秒。

### 2. 使用不透明一次性票据，不分发签名私钥

授权返回 256-bit 随机不透明票据。控制面只保存票据 SHA-256、claims 和状态，不保存原始 bearer。专用 NATS KV bucket 使用两分钟 TTL；消费通过 JetStream KV revision CAS 将 `Issued` 原子切换为 `Consumed`，并发时只能一个成功。

只有认证后的 `cn.vastplan.platform.security.credentials` 能调用 `kernel.configuration.authority.consume`。它拿到宿主派生 claims 后立即加密 material，并把 authority、coordinator、candidate、configuration 和 field 身份写入托管记录。原始票据不会进入协调器状态、凭证状态、候选 API、日志或浏览器响应。

### 3. 委托记录仍由配置候选驱动生命周期

凭证插件新增：

- `stageDelegated(authority, value)`：仅配置协调器可调用；owner/purpose/resource 只取消费后的 claims；
- `activateDelegated(stageId, candidateId)`；
- `abortDelegated(stageId, candidateId)`。

目标插件不能绕过候选直接激活委托记录。激活后，它仍可以按自己的 owner 身份退役引用。配置协调器私有状态保存 stage/ref 以恢复 Saga，但对 Portal 只返回字段 ID、是否已暂存和状态，不返回 handle、stage ID、authority 或密文。

创建带秘密 Draft 时先持久化 `Preparing` 候选，再逐字段签发、暂存并记录检查点，全部成功后进入 `Draft`。放弃 Draft 先进入 `RollingBack`，终止全部委托记录后才进入 `RolledBack`。失败状态不包含秘密或底层错误材料。

### 4. 语言与运行方式

本能力采用 Go：它位于 Backend Kernel、NATS CAS 和现有 Go 凭证状态机的强事务边界，Go 在并发、资源占用、静态契约和现有生态复用上优于 Node/Python。它不是普通业务插件 Runtime；签发/消费强制点属于可信宿主，协调器与凭证托管器继续以首方可信独立插件进程运行。

## 备选方案

- **自包含 JWT/Ed25519 授权**：跨服务读取方便，但需要新增集群签名私钥分发、轮换与 verifier 信任配置；JWT 本身也不提供一次性消费，仍需共享重放状态，否决。
- **HMAC token**：要求内核与凭证插件共享签名秘密，扩大高权限密钥边界，否决。
- **让协调器直接指定 owner/purpose**：实现简单但可越权，否决。
- **让凭证插件重新读取 Manifest**：会产生第二条制品信任和目录解析路径，且存在 TOCTOU，否决。
- **只靠随机 stageId**：不能证明该秘密对应哪个活动制品、Schema、候选和字段，否决。

## 影响

- 多内核、多服务并发下，同一配置授权只能消费一次。
- 新增一个短 TTL 控制面 bucket；Node/Manager 内核只可写自己 tenant 前缀，Runtime 进程无访问权。
- 凭证加密失败后已消费票据不能重试；协调器必须重新签发。这避免未知副作用下重复使用 bearer。
- 当前阶段完成安全暂存与放弃回滚，尚未把 Draft 发布为 Active；激活入口留给后续 Application Deployment、Platform Profile 和 hot 配置事务调用。
