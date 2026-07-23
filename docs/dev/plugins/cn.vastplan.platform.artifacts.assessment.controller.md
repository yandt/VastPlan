# Artifact Security Rescan Controller

该第一方平台插件实现 ADR-0139 的持续复扫控制面。它使用 Go 独立进程、`leader` 实例策略和 `leader-owned` 状态；不运行扫描器、不下载制品、不接触 Provider 私钥。Python/Node 对 CAS、leader 生命周期和低资源常驻循环没有优势，Rust 的额外工程成本暂不值得，因此选择 Go。

## 调度模型

每个 Controller 实例通过服务配置绑定一个 `tenantId`，可管理多个 channel；不同租户使用独立服务单元，避免后台循环在没有可信租户上下文时枚举所有租户。Controller 分页读取 active 且同时具有 SBOM、AdmissionRecord 的目录项，为每个精确 ref 在 service-scoped Shared State 保存独立计划。

Manifest 显式声明 `runtime.backgroundService=true`。安装器冻结该权限，Node Agent 从已验证服务配置把唯一 `tenantId` 绑定进 host-only LaunchPolicy；后台 goroutine 不持有长期 token，也不能自行构造 principal、凭证或其他租户。宿主只在插件 ACTIVATE 成功后接受自主 HostCall，并在 DRAIN、SHUTDOWN、断连或 Leader 外部调用 fence 失效时收紧调用面。

计划保存 tar/SBOM/Admission 摘要、策略 ID、最新 sequence/digest、数据库 revision、评估配置 revision、`nextScanAt`、重试次数和可选 pending StatusRecord。正常时间为 `expiresAt - leadTime - stableJitter(ref)`，抖动只把任务前移，不会越过安全提前量。Provider 数据库或评估配置 revision 变化会立即到期；临时失败使用有上限的指数退避和按 ref 稳定抖动。

## 故障与幂等

Controller 在调用 Repository 追加前，先用 fenced CAS 持久化 Provider 已签名的 pending record。进程崩溃或追加临时失败后复用同一记录，不重新扫描和改写同一 sequence。Repository 的只追加链是最终 CAS：旧 leader 与新 leader 即使在租约切换瞬间竞争，也只有符合当前前序摘要和 sequence 的记录能成功。

Runtime Host 在 leader lease 丢失后会立即阻止该 leader 插件继续发起外部能力调用；Shared State mutation 还必须通过 host-only execution fence。若追加已经成功但计划更新失败，下一轮从 Catalog 的已验证链头收敛并清除 pending，不重复追加。

## 运行边界

- Controller 必须依赖 Repository 与 Assessment Provider readiness，二者不可用时不推进计划。
- `status` 只返回目录 revision 和 eligible/deferred/succeeded/failed/conflict 计数，不以插件 ID 作为指标 label。
- 计划不保存扫描租约 URL、Material Lease、原始报告或任何 token。
- 当前不执行 soak；代表性插件进入仓库后，再验证大目录分页、数据库整批更新与长时间 leader 接管。

## 验证范围

真实子进程 E2E 使用实际 Plugin-Host 协议、NATS Shared State 与 execution fence，验证后台循环完成 Provider status、Repository Catalog、评估、pending 持久化、只追加提交和最终计划收敛。确定性故障夹具另行验证追加失败重试不重扫、追加已成功但最终计划保存失败时从 Catalog 链头恢复、数据库 revision 变化立即复扫；当前按约定不执行 soak。
