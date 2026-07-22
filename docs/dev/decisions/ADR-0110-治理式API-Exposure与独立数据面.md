# ADR-0110 治理式 API Exposure 与独立数据面

- 状态：已采纳，实施中
- 日期：2026-07-22
- 关联：[ADR-0021](ADR-0021-权限判定强制点.md)、[ADR-0025](ADR-0025-NATS控制面寻址与多节点调度.md)、[ADR-0068](ADR-0068-分布式平台管理中心与强类型BFF.md)、[ADR-0091](ADR-0091-制品存储Provider供给边界.md)、[ADR-0106](ADR-0106-多端统一身份授权与Runner执行租约.md)

## 背景

插件需要提供 HTTP API，但直接暴露插件 ID 会泄漏实现、绑定部署拓扑，并让认证、授权、Schema、限流和错误处理分散。人工别名会重名并产生长期维护成本；插件 ID 的截断 hash 仍有冲突、可枚举和实现替换导致地址变化的问题。制品仓库等服务还需要大对象或流式数据面，不能全部压入通用 RPC。

## 决策

1. 插件只声明不可变 `apiContracts`，route 最终绑定同一签名清单拥有的 `tool.package` operation；插件不能自行声明公网地址。
2. 平台新增治理式 `ApiExposure`，绑定 tenant、Portal/Host、认证 Profile、权限、限流、超时、逻辑服务和签名 Contract Reference。
3. 公开路径为 `/api/r/{routeKey}/v{major}/...`。Route Key 由平台生成 96-bit 随机值并编码为 20 位 Base32，小写、稳定、删除后墓碑化；不接受人工别名，也不从插件 ID 或 hash 派生。
4. Gateway 使用自包含 `ExposureCatalog`，其中完整契约与 Reference digest 相互校验。公开协议不得返回 plugin、capability、service、node 或内部 endpoint。
5. Gateway 做入口认证、租户/Portal 绑定、权限预检、限流、大小限制、请求/响应 Schema 与错误映射；Backend `Host.Invoke` 继续作为最终授权强制点，入口预检不能替代它。
6. 新增 `dataPlaneServices` 和短时 `EndpointLease`，支持 `gateway-proxy`、`ticket-redirect`、`private-direct`，以兼容独立 HTTPS 数据面。控制面故障时仅允许已签发且未过期的 Lease 继续工作。
7. 旧 `api.route` 保留在 Backend v1 兼容矩阵和 Schema 中，但标记为 deprecated；新插件、Node Gateway 和管理控制面不得依赖它。
8. 公共契约、校验和控制面优先 Go；现有 Node Portal Kernel 承载 HTTP Gateway；独立数据面服务保持按驱动生态选语言。运行方式与语言分别决策。

## 备选方案

- **人工别名**：可读但重名、改名、所有权与迁移治理成本高；拒绝。
- **插件 ID 截断 hash**：省去登记名称，但截断后仍需冲突表，且地址与实现耦合、可被字典反查；拒绝。
- **直接暴露插件 ID/capability**：实现最少，但泄漏内部结构并妨碍替换和集群路由；拒绝。
- **所有流量都由 Gateway 代理**：治理统一，但大对象与流式传输形成额外复制和瓶颈；普通 API 采用，数据面不强制。
- **所有插件自行监听端口**：灵活但扩大攻击面、进程数与证书/端口治理成本；仅允许已声明且持有短 Lease 的数据面服务。

## 影响

- 插件开发者需定义 bounded JSON Schema 和稳定错误码，但无需管理公网路径、认证 Token 或服务发现。
- Route Key 与插件无关，插件替换或重构不会破坏客户端地址。
- 控制面必须维护 Exposure 生命周期、Route Key tombstone、职责分离审批、Catalog generation 和 Endpoint Lease。
- Portal、Mobile、Runner 与服务客户端使用相同 Token/身份验证原则；不同入口只负责载体校验，最终权限仍由统一 Authorization Policy 与 Backend PEP 裁决。

