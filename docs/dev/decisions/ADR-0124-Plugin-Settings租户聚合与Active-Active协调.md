# ADR-0124 Plugin Settings 租户聚合与 Active-Active 协调

- 状态：已采纳并实现
- 日期：2026-07-23

## 背景

`plugin-settings` 同时保存配置候选、异人审批、凭证阶段、Application/Profile/Service Hot/Scoped Hot/Resource 激活 Saga 和审计。旧实现把所有租户写入一个本地文件，只能以 leader 运行；节点故障时无法在其他节点继续恢复。Shared State v1 只有单 key CAS，没有跨 key 事务，因此不能把一次工作流的状态机械拆成多个独立 key。

## 决策

1. 采用 Go，继续运行在既有可信 Go 插件进程。该插件的大量状态机、Catalog 校验和凭证协议已经是 Go；迁移只改变持久化与副本模型，改用 Node/Python 不会带来生态优势，反而扩大重写和安全验证范围。
2. 每个 tenant 的完整协调状态保存为 `tenant/configuration.coordinator/tenant` 单文档。候选、current 锁、凭证阶段、激活记录和审计在一次 Shared State CAS 中提交，保持旧原子边界。
3. 每次入口调用先从 Shared State 读取租户快照；每个内部检查点使用上次返回的 Store revision 更新。并发副本的 stale writer 返回可重试 conflict，不能静默覆盖。
4. 插件改为 `active-active + external-shared + queue`。清单只申请 get/create/update，不申请 Delete/List；tenant、plugin ID 和 RuntimeScope 仍由可信宿主派生。
5. 工作流发生外部副作用后若 CAS 冲突，现有 status/prepare/commit/abort 协议负责幂等恢复，不能根据本地超时猜测外部失败。
6. Scoped watch 保留本实例即时通知，并每秒从 Shared State 重新解析一次，用于发现其他副本提交；响应仍只包含 revision/digest，不携带 values。
7. 单文档继续受 1 MiB 上限约束。达到容量上限时 fail-closed；未来只有在真实容量数据证明必要后，才以“根提交指针 + 分片记录 + 显式恢复 Saga”升级，不能在 v1 上伪造跨 key 事务。
8. 当前处于开发阶段，不迁移旧 `platform.plugin-configuration.stateFile` 数据；清单和开发 Profile 直接删除该配置。生产历史数据形成后必须另写在线迁移 ADR。

## 影响

正面：任一副本都能处理请求和继续 Saga；节点重启不丢失协调状态；跨租户状态物理隔离；并发写由 Provider CAS fencing；不再需要插件私有状态目录。

代价：同一 tenant 的写入仍在一个聚合上竞争；长工作流可能因无关并发写返回可重试 conflict；watch 每个等待请求最多每秒产生一次共享状态读取；单租户容量必须监控。

## 验证

- 原有 Draft、五类激活、凭证补偿与重启恢复测试继续通过；测试专用 File snapshot 不进入生产二进制路径。
- 新测试覆盖两个 Service 实例共享读取、tenant 隔离、可信身份不进入请求和并发 CAS 单赢家。
- Scoped watch 同时覆盖本实例通知和跨实例轮询路径；Shared State 不可用时 fail-closed，不回退本地文件。
