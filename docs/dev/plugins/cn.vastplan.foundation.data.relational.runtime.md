# Database Runtime 基础插件

插件 ID：`cn.vastplan.foundation.data.relational.runtime`

能力：`tool.package/foundation.data.relational.runtime`

当前制品版本：`0.2.0`

## 职责边界

Database Runtime 是关系数据库数据面，负责 Provider、节点本地连接池、查询、事务、健康和指标；它不管理 Portal 中的连接定义，也不拥有 CredentialRef 生命周期。连接管理面仍属于 `cn.vastplan.platform.data.relational.connection-manager`，Backend Kernel 只负责通用 capability 路由、可信身份和进程生命周期。

它与 ADR-0089 的语言 Runtime Provider 不是同一概念。Database Runtime 使用 native 独立进程，不把数据库 session 和短期 material 放入 Kernel 或无关插件的地址空间。

## v1 wire 契约

机器可执行真源位于 `contracts/schemas/database/v1`，预留以下操作：

| 操作 | 语义 | 0.2.0 状态 |
|---|---|---|
| `providers` | 返回当前签名制品内冻结的 Provider descriptor | 已开放 |
| `probe` | 使用候选连接定义做连通性检查 | 契约已固化，未开放 |
| `activate` / `retire` | 创建新池 generation、排空旧 generation | 契约已固化，未开放 |
| `query` / `execute` | 以 ConnectionRef 执行参数化语句 | 契约已固化，未开放 |
| `begin` / `commit` / `rollback` | 管理带实例亲和的不透明事务句柄 | 契约已固化，未开放 |

值使用显式 `null/string/int64/decimal/bool/bytes/timestamp/json` 类型。`int64` 和 `decimal` 采用字符串编码，避免 JavaScript、Go、Python 等语言之间发生精度漂移。连接载荷拒绝 DSN/URL 和疑似密码、token、private key 配置，只接受非敏感 endpoint/options 与托管 CredentialRef。

稳定错误码统一使用 `database.runtime.*`。ADR 中的 `TRANSACTION_LOST` 在 wire 上对应 `database.runtime.transaction_lost`。

## Provider SPI

第一方 Provider 实现 `Descriptor/Validate/OpenPool`；Pool 实现 `Probe/Query/Execute/Begin/Stats/Close`，Transaction 实现查询、执行、提交与回滚。Registry 在注册时完成 descriptor 与配置 Schema 校验并冻结副本，拒绝重复 ID、typed-nil、`psql` ID、外部 Schema `$ref` 和注册后 descriptor 漂移。

Provider 不接收口令字段，只保留 `MaterialSource`。每次创建物理连接时通过短期回调使用 material，不能保留回调字节。该 SPI 只用于同一第一方签名制品内部；未来第三方 Provider 经独立进程和版本化 RPC 接入。

## 可信 material 路径

Backend Host 从已验签 `LaunchPolicy` 生成 host-only Runtime identity，绑定插件 ID、发布者、版本、制品 SHA-256、节点、service unit 和每次启动随机实例。完整 identity 不进入 wire；插件只接收其非秘密 audience 摘要。Database Runtime 每次使用凭证生成一次性 X25519 接收密钥，经声明的 `kernel.credential.material-lease` 请求加密信封。

Kernel 只允许当前认证会话调用，并只为首方 Database Runtime、中继 connection-manager 所拥有的 `database.connection` 引用；它不创建接收私钥，也不解封 material。跨服务凭证插件继续只接受 transport-trusted `SYSTEM` 调用并把同一 audience 写入 AAD。Runtime 校验 tenant、audience、完整 CredentialRef、TTL 和 GCM 后在同步回调中使用并清零明文。错误 audience、跨 tenant、跨 owner、过期 lease 和伪造 caller 均 fail-closed。

## 当前限制与下一阶段

0.2.0 的启动进程仍注册空 Provider Registry，只安全开放 `providers`，因此返回空列表且不能打开数据库连接。真实数据操作继续 fail-closed。

下一阶段实现池管理器、资源预算、generation 轮换与指标，再同批接入 `postgresql` 与 `mysql` 两个真实 Provider。完整路线见 [ADR-0095](../decisions/ADR-0095-Database-Runtime多Provider连接池与集群事务.md)。
