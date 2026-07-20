# Database Runtime 基础插件

插件 ID：`cn.vastplan.foundation.data.relational.runtime`

能力：`tool.package/foundation.data.relational.runtime`

本插件是关系数据库数据面，不是连接配置管理页，也不是承载 Go/Node/Python 插件的语言 Runtime。它最终负责 Provider 注册、本地连接池、查询、事务、健康和指标；`cn.vastplan.platform.data.relational.connection-manager` 继续拥有连接定义与 CredentialRef 生命周期。

## 当前阶段

0.3.0 已完成 Database Runtime v1 wire 契约、Provider SPI、可信运行实例 identity、加密 Material Lease 中继，以及统一 Pool Manager。Pool Manager 已具备节点/租户/连接三级预算、调用方并发和等待队列上限、revision/generation 原子切换、旧池有界排空、关闭失败保守占额和无敏感标识指标；这些生命周期由 fake Provider 的并发与故障测试验证。

`probe/activate/query/execute/begin/commit/rollback` 已进入机器可执行契约，但真实 Provider 和执行服务尚未接入，因此仍不会写入插件 descriptor，也不会接收外部请求。当前制品可以安全启动和报告空 Provider 列表，但不能打开物理数据库连接；这属于明确的 fail-closed，不是可用数据库服务。下一阶段必须同批接入 PostgreSQL 与 MySQL Provider，不能把 `psql` 当作 Provider ID。

## 安全边界

- 部署时使用 native 独立进程；不与无关插件共享地址空间。
- Provider 配置不得包含 DSN、密码、token、private key 等秘密，只能引用托管 CredentialRef。
- Host 会话把首方发布者、制品摘要、节点、unit 和单次启动实例绑定为不可伪造的 Runtime audience；插件不能从 payload 自报身份。
- Provider 只保留 `MaterialSource`，需要创建物理连接时才生成一次性 X25519 密钥并取得加密 lease；Kernel 不持有私钥、看不到明文，Provider 不得缓存回调中的字节切片。
- Pool key 和快照不包含 endpoint、CredentialRef 或 material；租户、项目和连接资源只以短摘要进入内部指标。
- 新 generation 创建前按轮换重叠量预留预算；旧 generation 关闭失败时继续占额，避免实际连接数失控。
- 第三方 Provider 不实现本 Go SPI，而是经未来的隔离进程和版本化 RPC 接入。

设计依据见 [ADR-0095](../../../docs/dev/decisions/ADR-0095-Database-Runtime多Provider连接池与集群事务.md)。
