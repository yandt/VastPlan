# ADR-0025 NATS 控制面、能力寻址与多节点调度

- 状态：已采纳
- 日期：2026-07-16
- 关联：[ADR-0008 骨架技术选型对比](ADR-0008-骨架技术选型对比.md)、[ADR-0010 插件服务与部署编排](ADR-0010-插件服务与部署编排.md)、[ADR-0024 单节点自动装配与回滚语义](ADR-0024-单节点自动装配与回滚语义.md)、[系统架构 §2/§3](../architecture/系统架构.md)

## 背景

ADR-0024 已固定单节点装配的事务、幂等和回滚语义，但 NATS KV、真实进程健康、drain、跨节点寻址、节点租约和多副本放置仍未落地。若让每个 Node Agent 直接读取同一份全局配置并各自选择副本，会在成员视图不一致时重复启动；若把所有调用都强制序列化，即使同进程也会破坏位置透明的性能目标。

## 决策

1. **NATS 是控制面与默认 mesh 数据面**。JetStream KV 分 bucket 保存全局 Deployment v2、每节点 DesiredState v1 assignment、节点实际态、节点租约与 capability 租约；NATS request-reply 承载远端调用，Core NATS 承载非持久事件。生产 bucket 的 JetStream 副本数至少为 3。
2. **寻址以 capability 为稳定身份**。本地 Router 命中时函数直调、零序列化；未命中时查本地缓存的 capability KV 目录，再向 `vp.rpc.v1.<capability-token>` 发 Protobuf request-reply。所有同 capability 实例加入同一 queue group。应用错误继续放在 `CallResult`，wire/handler 故障返回独立 `TransportError`；调用取消经 NATS 传播到远端 handler。
3. **能力目录按实例租约登记**。Runtime 只发布 `single` 扩展点的插件贡献，不发布内核私有能力、select/fanout/mount 贡献；候选宿主完成握手和激活后才登记，切换时先摘除旧租约，再 DRAIN 在途调用并回收旧进程。租约 TTL 负责异常节点最终摘流。
4. **全局输入与节点执行契约分层**。`deployment/v2` 是 Plugin Service 控制器接收的全局 service 部署规格，支持整数 `replicas >= 1` 和精确 `nodeSelector`；控制器把它展开成每节点一份保持兼容的 `deployment/v1` assignment。Node Agent 不参与全局副本仲裁。
5. **放置采用 rendezvous hashing**。每个 unit 在匹配标签的不同节点上至多放一个副本，节点集合小幅变化时尽量少迁移。匹配节点少于副本数时在写 assignment 前 fail-closed，不发布半份计划。亲和、资源计分和同节点多副本不在本版本内。
6. **assignment 使用独立、持久化 generation**。业务 Deployment revision 表示配置历史；节点成员变化也会改变 assignment，因此控制器在 KV 中 CAS 保留单调 generation，节点执行快照使用 generation 作为 revision。多节点写入是可重试的最终一致操作，不把 KV 冒充跨 key 事务数据库。
7. **节点租约同时是调度成员资格与自我隔离信号**。节点每 5 秒续租，KV TTL 为 30 秒；连续 15 秒无法续租时 Node Agent 停止本机 unit。复用旧 node id 启动时先作废该节点遗留 assignment，再发布新租约，等待控制器重发当前计划，避免先执行陈旧快照。
8. **真实运行事实驱动恢复**。Runtime 用插件会话/进程存活而非历史 PID 判断健康；进程退出立即触发 reconcile，失败采用指数退避和抖动。候选失败保留旧实例，完全收敛并保存实际态后才回收未引用安装内容。

## 备选方案

- **每个 Node Agent 独立计算全局放置**：少一个控制器，但成员视图短暂分叉会造成超额副本，且难以审计一次调度决定。拒绝。
- **修改 deployment/v1 直接允许多副本**：会把“全局意图”和“节点可执行快照”混成同一语义，旧代理可能误读。保留 v1，新增 v2 全局契约。
- **所有本地调用也走 NATS**：实现路径单一，但同进程付出序列化、broker 和网络栈成本。拒绝，本地直调与远端 wire 共享业务签名。
- **引入 Kubernetes scheduler/etcd**：成熟但把裸机、气隙与边缘部署变成二等公民，也扩大当前依赖面。维持自管轻量控制器；Kubernetes 仍可作为承载环境。

## 影响

- 正面：已形成“全局 Deployment → 节点租约 → 确定性 assignment → Node Agent 装配 → capability mesh → 实际态”的真实多节点闭环；扩缩容、故障漂移、queue group 与跨节点真实插件调用均有 E2E。
- 代价：assignment 跨 key 更新只保证最终一致，拓扑切换期间可能短暂少一个或多一个可见实例；调用方必须保持超时、幂等与重试纪律。
- 边界：当前只调度无状态 service、整数副本、精确标签且每节点最多一个同 unit 副本；流式 RPC、持久事件、资源容量、亲和/反亲和、控制器 leader election、NATS TLS/账号权限和远端制品签名另行决策。
