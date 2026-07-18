# ADR-0036 Backend 核心 SPI 边界

- 状态：已采纳
- 日期：2026-07-16
- 关联：[ADR-0031 Backend 内核 1.0 封板与工程门禁](ADR-0031-Backend内核1.0封板与工程门禁.md)、[ADR-0033 Backend 插件状态迁移事务](ADR-0033-Backend插件状态迁移事务.md)

## 背景

Node Agent 已有实际态 `StateStore`，但它是控制面恢复实现，不是插件业务持久化 SPI。`CallContext` 也只有凭证句柄，没有可信宿主如何解析和短时使用凭证的接口。若插件直接读取环境变量、Vault 或数据库，会绕过租户/插件隔离并把基础设施产品固化进插件。

## 决策

1. `core/shared/go/kernelspi` 是配置、凭证、持久化和事务的 Go 侧单一真源；`Dependencies` 由部署适配器注入每个 Host，nil 能力 fail-closed。
2. 所有有状态操作必须携带 `Scope{tenant, project, plugin, namespace}`。tenant、plugin、namespace 必填；插件 ID 来自已认证协议会话，不能从插件 payload 读取。
3. `ConfigProvider` 返回冻结的 JSON 值。Runtime 为每个 unit 深拷贝配置并注入候选 Host；插件经 `kernel.config.get` 读取，服务只接受 `CALLER_KIND_PLUGIN`。
4. `CredentialBroker.WithCredential` 只把 material 暴露给可信宿主回调，不能序列化成协议响应。插件请求的是使用凭证的宿主操作，而不是读取明文。
5. `Persistence` 提供 scoped get/put/delete；`TransactionManager.Begin` 返回同 scope 的事务，commit/rollback 都显式且关闭后不可复用。内存实现验证 copy-on-write、rollback 和乐观冲突语义，生产实现由数据库适配器提供。
6. HostCall 覆盖插件自报的 `Caller`，强制注入会话握手得到的插件 ID，同时保留 tenant/project/principal/trace。这样权限和 SPI scope 依赖的是宿主事实。

## 备选方案

- **复用 Node Agent StateStore**：它保存部署实际态，没有租户、插件 namespace 和业务事务语义。拒绝。
- **只提供跨进程 JSON API**：部署层无法以类型安全方式替换实现，协议与实现会漂移。拒绝。
- **把凭证明文返回插件**：实现简单但违反 CredentialRef 设计并扩大泄漏面。拒绝。
- **插件直接连接数据库/Vault**：绕过宿主策略、审计与隔离。拒绝作为内核默认路径。

## 影响

- 正面：配置和存储产品可替换，凭证不出宿主，事务语义可测试。
- 代价：部署适配器必须提供生产 CredentialBroker、Persistence 和 TransactionManager；内核不假装内存实现可用于多节点生产。
- 风险：未来新增跨进程 persistence 服务时必须绑定会话 scope 和事务所有者，不能信任请求里的 plugin ID。
