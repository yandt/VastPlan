# ADR-0156 Authorization Policy 共享真相源与 Leader 接管

- 状态：已采纳，已实施
- 日期：2026-07-26
- 关联：[ADR-0044](ADR-0044-全局依赖编排与本地自治启动管理.md)、[ADR-0107](ADR-0107-插件权限目录与系统管理授权治理.md)、[ADR-0123](ADR-0123-插件共享状态与可信Provider.md)、[ADR-0153](ADR-0153-Kernel-Service可信授权编译.md)

## 背景

`authorization-policy` 虽按 leader 路由，但 Role、Binding、撤权、审批和审计仍保存于 leader 本机 JSON 文件。leader 约束只能避免同时写，不能让新节点接管旧节点状态；故障漂移可能形成空策略、旧策略或 generation 回退。让插件直接连接 NATS KV 又会绕过内核的可信 Runtime 身份、作用域、Grant、fencing 和审计。

## 决策

1. Policy 治理账本迁移到 `kernel.state.shared.v1`，固定使用 `service` scope、私有 namespace `authorization.policy` 和 key `active`。插件只调用 Host，不获得 NATS 凭据、bucket 名和物理 key。
2. 服务继续使用 leader routing，但 `stateModel` 改为 `external-shared`。读取走普通 Shared State，create/update 必须走 fenced 服务；旧 leader 即使仍存活也不能提交写入。
3. State 内的业务 `generation` 与 Shared State entry revision 分离。写入先读取当前 entry，核对业务 generation，再用 entry revision 完成 Store CAS；两层任一冲突都拒绝。
4. 开发 Seed 可提供 owner-only 本地 bootstrap state。它只在权威 key 不存在时导入一次，并把 Store generation 重建为 1；权威 key 已存在或 Shared State 不可用时都禁止读取本地文件作为回退。
5. 运行配置通过 Manifest 申请、Platform Profile 精确 Grant 和发布者上限三层授权 `get/fenced.create/fenced.update`。缺失 Grant 时插件在激活前失败。
6. 本 ADR 不把签名私钥或 Policy 私有 State 暴露给 Enforcer。签名 Snapshot、Trust 与 LKG 的跨节点发布需要独立的签名发布/订阅协议；当前本地文件物化保持不变并明确列为后继工作。

## 备选方案

### 继续使用 leader 本地文件

改动最小，但新 leader 无法获得旧账本，否决。

### 插件直接连接 NATS KV

能共享数据，却复制连接、身份和授权逻辑，且第三方 Runtime 容易形成旁路，否决。

### Shared State 故障时回退本地文件

短时可用性更高，却可能在恢复后把旧状态覆盖新状态；对授权真相源属于 split-brain，否决。

## 影响

- Policy 进程重启或 leader 迁移后可读取同一治理账本；本机磁盘不再是运行期真相源。
- Shared State 或 leader fence 不可用时，管理写入 fail-closed；已部署 Enforcer 仍按自身未过期 LKG 规则判定。
- 开发环境已有文件不会被持续双写，也不能成为灾难恢复副本；正式备份必须使用 Shared State 备份流程。
- A3 封闭的是治理账本高可用，不宣称已完成跨节点 Snapshot 分发。
