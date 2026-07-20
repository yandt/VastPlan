# Database Runtime 基础插件

插件 ID：`cn.vastplan.foundation.data.relational.runtime`

能力：`tool.package/foundation.data.relational.runtime`

本插件是关系数据库数据面，不是连接配置管理页，也不是承载 Go/Node/Python 插件的语言 Runtime。它最终负责 Provider 注册、本地连接池、查询、事务、健康和指标；`cn.vastplan.platform.data.relational.connection-manager` 继续拥有连接定义与 CredentialRef 生命周期。

## 当前阶段

0.1.0 只完成 Database Runtime v1 wire 契约、Provider SPI、错误分类、注册表和 `providers` 发现操作。`probe/activate/query/execute/begin/commit/rollback` 已进入机器可执行契约，但在可信运行实例 identity 和 Material Lease audience 完成前不会写入插件 descriptor，也不会接收请求。

因此当前制品可以安全启动和报告空 Provider 列表，但不能打开物理数据库连接；这属于明确的 fail-closed，不是可用数据库服务。后续必须同批接入 PostgreSQL 与 MySQL Provider，不能把 `psql` 当作 Provider ID。

## 安全边界

- 部署时使用 native 独立进程；不与无关插件共享地址空间。
- Provider 配置不得包含 DSN、密码、token、private key 等秘密，只能引用托管 CredentialRef。
- Provider 只保留 `MaterialSource`，需要创建物理连接时才在短期回调内取得 material；不得缓存回调中的字节切片。
- 第三方 Provider 不实现本 Go SPI，而是经未来的隔离进程和版本化 RPC 接入。

设计依据见 [ADR-0095](../../../docs/dev/decisions/ADR-0095-Database-Runtime多Provider连接池与集群事务.md)。
