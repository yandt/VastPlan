# ADR-0128 统一 Leader Epoch 与外部副作用 Fencing

- 状态：已采纳，第一阶段已实现
- 日期：2026-07-23

## 背景

Backend 已为 `leader`/`partitioned` Unit 维护 JetStream KV 领导权、单调 epoch 和随机 fencing token，但此前这些证据只出现在 capability announcement。Deployment Manager 调用 SSH、Deployment 发布和 Platform Profile 激活时，内核只校验插件 ID；失去领导权的旧进程在短暂重叠窗口内仍可能发起或继续外部副作用。Shared State Root CAS 只能拒绝旧账本写入，不能撤销已经发出的远程操作。

## 决策

1. 不建立第二组选主或“插件自定义启动 lease”。Node Agent 现有 Unit Leadership 是唯一 epoch/token 真源；正常交接和故障接管继续保证 epoch 单调递增。
2. Runtime Host 在取得领导权后绑定 host-only `operationfence.Evidence`。Evidence 只存在 Go `context`，不进入 `CallContext`、插件 payload、环境变量、日志或 API；插件启动完成但尚未取得 lease 时，副作用回调 fail-closed。
3. Host 每次调用内核服务时动态询问 Leadership 是否仍 current。领导权释放、续租失败或超过 lease duration 后不再注入 evidence；Node Agent 同时停止宿主，会取消所有在途 HostCall。
4. Deployment Manager 的 mutating kernel callbacks 必须同时满足：认证 caller、可信 RuntimeIdentity、RuntimeScope 与 UnitID 一致、逻辑服务为 `platform.deployment`、当前 epoch/token 有效。只读 targets/preview/status/readiness 不要求副作用 fence，但仍保留原身份与租户校验。
5. 业务操作使用稳定 OperationID：Bootstrap 使用持久 Job ID；Deployment 发布使用 deployment + revision；Profile 激活使用 candidate + action。可信宿主把 OperationID 与 host-only evidence 组合成 `Fence`，插件不能提交 epoch/token。
6. Deployment/Catalog 写入继续使用已有单调 revision、request digest 和 KV CAS。Host fence 阻止旧 leader 开始调用；上下文取消和底层 CAS 阻止延迟提交，不复制第二套业务 revision。
7. SSH 首次引导额外携带 fence epoch 和 OperationID 到固定脚本。远端在 root-owned `/var/lib/vastplan-fencing` 使用 `flock` 串行引导，先拒绝低于已记录 epoch 的请求，再原子写入 epoch/operation marker。token 不落盘；相同 epoch 的重试允许执行固定幂等脚本，较低 epoch 永久拒绝。
8. 第一阶段仍保持 Deployment Manager 单 Leader。该能力提供可靠 active-passive 接管，不自动意味着 active-active；若未来拆分多写者，必须按操作/目标重新定义 owner，而不能复用一个不存在的全局 leader token。
9. 核心实现使用 Go：LeaderElector、Node Agent、Runtime Host、SSH Broker 与 Deployment/Catalog Controller 均在 Go，且 evidence 必须留在可信进程。未来远端 Provider 若跨语言，只接收窄化、签名的 provider protocol，不暴露原始 leader token给普通插件。

## 备选方案

- **在 Deployment Manager Shared State 再建 operation lease**：产生两个 owner 真源及交接顺序问题，拒绝。
- **把 token 放进 CallContext 或插件请求**：插件可见并可能重放、泄漏或错误传播，拒绝。
- **只依赖 Root CAS**：无法约束 SSH 等外部副作用，拒绝。
- **只在进程失效后 kill**：远端命令可能已开始，缺少远端 epoch/锁，拒绝。
- **所有操作要求独立分布式锁**：Deployment/Catalog 已有业务 CAS，重复加锁扩大死锁与恢复面；仅 SSH 增加远端串行 fence。

## 影响

正面：选主、能力路由、内核授权和 SSH 远端执行共享同一 epoch；旧 leader 在交接后不能开始新副作用；在途 SSH 可由上下文取消，延迟旧 epoch 在远端拒绝；插件无法接触 bearer token。

代价：leader 插件在取得 lease 前不能调用 mutating kernel service；SSH 目标需提供 `flock`；远端保留 root-owned fence 目录。Platform/Catalog Provider 仍需持续保持其 revision/CAS 不变量。

## 验证

- Leadership `Current()` 在活动期有效，Close 后同步失效；
- Runtime Host 只在 Provider current 时向 HostService 注入 evidence；
- SSH、Deployment 发布和 Profile 激活缺少 evidence 均拒绝；
- SSH 脚本包含独占锁、epoch 回退拒绝和原子 marker；
- 跨实例接管获得更高 epoch/不同 token；
- 全量 Go、race、cluster E2E、Portal 生命周期与发布门禁共同验证。
