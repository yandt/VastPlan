# ADR-0155 Browser/Server 单一 Generation 协调提交

- 状态：已采纳，已实施
- 日期：2026-07-26
- 关联：[ADR-0078](ADR-0078-Frontend事务式热替换与插件生命周期.md)、[ADR-0097](ADR-0097-测试制品仓库与前端分级热升级.md)、[ADR-0105](ADR-0105-可信多文件前端模块图与双端Generation.md)

## 背景

Browser `PortalGenerationManager` 与 Node `ServerGenerationManager` 原本各自具备候选准备、健康校验、失败保留旧代和提交能力，但提交点彼此独立。Server Worker 会在首次 SSR 请求中隐式准备并立即成为活动代；Browser 则在 RuntimeSpec 下载、模块验签和状态恢复后独立切换。Activation 更新期间可能短暂形成不同 revision，且 Browser 候选失败不能阻止 Server 已经切换。

Web 两端不能在网络上实现物理同时写入。目标应是一个可线性化、可重试、失败安全的提交点，而不是伪装成分布式强原子事务。

## 决策

### 1. Server Generation 拆成 prepare、commit、render-active

`ServerGenerationManager.prepare` 只物化密封 Server Graph、启动受限 Worker 并执行健康 render，候选保持不可见。只有 `commit` 能替换当前 Server Generation；`renderActive` 只在当前 key 精确匹配 tenant、Portal、revision 和 Server Graph digest 时渲染，不会隐式创建或切换 Worker。

冷启动或新 Activation 尚未协调提交时，SSR 返回 bypass 并交由 CSR 启动。不得为了首屏 SSR 绕过双端事务。

### 2. RuntimeSpec 携带宿主生成的短时协调事务

Node 在返回活动 RuntimeSpec 前准备同 revision 的 Server 候选，并投影短时随机 `coordination.transactionId`。事务仅存在于 Node 私有内存，绑定 tenant、Portal、Activation 和精确 Server Generation key；同一 slot 只保留一个待提交事务，较旧 Activation 不能覆盖较新候选。未提交事务到期后销毁 Worker 和临时物化目录。

如果同一精确 Server Generation 已经提交，RuntimeSpec 只返回 `state=committed`，不再要求 Browser 写请求。没有 Server Graph 的纯 CSR Portal 不携带协调字段。

### 3. Browser 验证完成后进入唯一提交点

Browser 仍先完成签名模块图下载、实际字节摘要复核、模块导入、Portal 装配、旧状态 capture 和候选 restore。随后 `beforeCommit` 通过会话与双提交 CSRF 调用固定 `/v1/portal-generation-commit`。Node 再读取权威 Activation，验证 tenant、Portal、revision 与 audience 没有变化，才提交 Server Generation。

Node 成功响应是线性化点：响应前 Server 已提交；响应后 Browser 才替换活动 Generation 并处置旧代。响应丢失、CSRF 失败、事务过期、Activation 漂移或 Server commit 失败时 Browser 候选被销毁，旧 Browser Generation 保持活动。提交在短时保留窗口内幂等。

### 4. Recovery 与 Host Epoch

Recovery Runtime 不把历史 fallback 提升为当前 Server Generation，继续以安全 CSR 恢复。Host Epoch 的 preflight 只验证 Browser 候选，不提交 Server；文档刷新后重新获取当前 RuntimeSpec，再按正常协调事务提交。开发态内存 HMR 没有密封 Server Graph 时继续使用 Browser-only Generation。

### 5. 多 Node 语义

协调事务是 Node 本地资源，和该节点本地 SSR Worker 一一对应。生产多副本入口应对 RuntimeSpec→commit 的短事务窗口启用会话亲和；错误路由到其他节点会 fail-closed 为 `transaction_not_found`，不会让 Browser 单独切换。未提交该 revision 的其他 Node 对 SSR 只会 bypass，不会渲染旧 revision，因此不会产生错误的跨代 HTML。共享协调 Provider 可在真实多副本容量与入口行为验证后引入，但不得改变上述精确绑定和线性化语义。

## 备选方案

### Server 候选准备后立即提交

延迟最低，但回到两端独立提交，Browser 模块或 restore 失败时 Server 已换代，否决。

### Browser 先切换再通知 Server

页面更新看似更快，但 Server 提交失败后 Browser 已经不可逆地暴露新代，否决。

### 让 SSR 请求隐式推进提交

会把普通读取变成隐藏状态变更，也无法证明 Browser 候选已通过，否决。

### 引入跨节点分布式两阶段提交

能统一多 Node 状态，但会把页面热替换变成依赖共享事务协调服务的高成本控制面，并引入阻塞恢复问题。当前先采用 Node 本地、失败时 SSR bypass 的安全模型。

## 影响

- Browser/Server 不再出现由独立提交造成的 revision 交叉；
- 首个冷请求可能没有 SSR，但不会以错误 revision hydrate；
- 每次未提交的新 Server 候选最多存活两分钟，并按 Portal slot 合并；
- 新增一次仅在 `prepared` 状态发生的 CSRF 提交往返；已提交代、纯 CSR 和本地偏好重组不增加请求；
- 生产多 Node 入口需要在短提交窗口配置会话亲和，并纳入部署验收。
