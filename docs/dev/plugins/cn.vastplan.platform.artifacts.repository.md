# 制品仓库基础插件

插件 ID：`cn.vastplan.platform.artifacts.repository`
当前制品版本：`0.26.0`

仓库的数据面由存储 Provider 在配置/启动阶段供给。当前开发组合使用 `cn.vastplan.platform.artifacts.storage.file`，仓库状态 API 会返回实际 `storageProvider`；对象发布和读取仍直接使用已供给的本地数据面，不逐对象调用 Provider。设计原因见 [ADR-0091](../decisions/ADR-0091-制品存储Provider供给边界.md)。

能力：`tool.package/platform.artifacts.repository`

## 边界

该第一方基础插件运行 HTTPS 制品发布与读取服务，负责 HTTP 传输、分离操作令牌、可重建 Catalog、单调 Publish Journal、确定性依赖解析、`deprecated/yanked/revoked` 生命周期、消费者引用快照、离线 Bundle、累积配额与容量统计、可回滚 File Volume 迁移，以及 fail-closed 的 `plan -> quarantine -> sweep` 垃圾回收。0.26.0 增加插件制品安全准入 sidecar：仓库与 Node Agent 共同复验外部 Provider 签发、绑定 tar/SBOM/策略的漏洞与许可证评估，Catalog、stable 审批、Bootstrap 镜像和离线 Bundle 均保持同一原始记录。对象存储与 OCI 通过供给 Provider 增加；审批和市场 API 仍在仓库领域扩展。

插件**不拥有信任解释权**：每次发布都交给内核 `SignedRepository` 校验清单、SHA-256、发布者证明、撤销状态和不可变版本；每次读取也只转发内核已验证的包与原始证明。Node Agent 对从任何来源取得的 `Envelope` 仍会在自己的强制点再次验证，不能把本服务的 HTTPS 或“已读取”当作可信标志。

签名 Manifest 声明读、生命周期、GC、迁移、提交发布和批准发布六类系统管理权限，并把 22 个面向用户的 operation 精确绑定到权限、风险和访问类型。提交与批准使用不同权限且由插件再次强制双人分离。内部 `putReferences` 由 Node Agent/部署控制器的 workload 身份授权，不进入人员角色目录。Portal Binding 允许访问仓库不等于用户获权，Backend PEP 仍执行最终判定。

当前以 `leader / leader-owned / cluster` 运行。集群成员可通过 `platform.artifacts.repository` 查询状态，数据复制与多活对象存储尚未在本版本提供，不能把多个实例指向不同本地目录后宣称高可用。

本地平台开发组合已经把它与临时 Seed 仓库分离：Seed 只负责本次启动的基础制品，本插件使用 `.vastplan/dev-platform/repositories/testing/volumes/repository.primary` 作为跨普通重启保留的测试数据面。Node Agent 将 Seed 作为优先 bootstrap source，将本插件的 HTTPS 端点作为普通远端 source；Node Agent 通过本次运行的组合信任快照复验两者，而本插件自身只加载 testing-only 信任文档，不接受临时 Seed 身份发布的制品。

仓库关键栈在线升级由 Node Agent 内的可信宿主适配器负责，而不是本插件自升级。本插件无权读取或写入 Seed 与 Bootstrap Inventory。候选证明与内容先经过内核固定验证点，再镜像到 Seed；只有候选 Runtime 健康且活动 Assignment 引用发布成功后才推进 LKG。自动路径拒绝跨 channel 和 SemVer 降级，详细事务见 [ADR-0102](../decisions/ADR-0102-可信宿主仓库自升级事务.md)。

## 运行配置

签名清单声明的非敏感插件配置通过调用方隔离的启动快照注入：

| 字段 | 含义 |
|---|---|
| `listen` | HTTPS 监听地址；默认 `127.0.0.1:8443` |
| `storageProvider` | 已供给当前数据面的 Provider 能力 ID |
| `volumeId` | 部署配置指向的活动 volume ID；迁移 finalize 后必须更新它并重启，才允许 release 旧卷 |
| `quota.maxArtifacts` / `quota.maxBytes` | 可选全仓活动制品数量/对象字节上限；`0` 表示该维度不限额 |
| `quota.rules[]` | 可选累积规则；每条以稳定 `id` 按 namespace、publisher、channel 中至少一个维度匹配，并设置数量和/或字节上限 |
| `apiExposure` | 可选 Data Plane Exposure 接入；包含已发布 `exposureId`、稳定 `instanceId`、外部 HTTPS `endpoint` 与 `tlsIdentity` |

部署适配器仍需向该第一方进程提供以下受控挂载/秘密；它们不属于普通插件设置：

| 变量 | 含义 |
|---|---|
| `VASTPLAN_ARTIFACT_REPOSITORY` | 不可变本地制品存储根目录 |
| `VASTPLAN_ARTIFACT_TRUST` | 发布者 Ed25519 信任文档 |
| `VASTPLAN_ARTIFACT_TLS_CERT` / `VASTPLAN_ARTIFACT_TLS_KEY` | TLS 证书与私钥 PEM |
| `VASTPLAN_ARTIFACT_READ_TOKEN` | 制品读取 Bearer token |
| `VASTPLAN_ARTIFACT_PUBLISH_TOKEN` | 制品发布 Bearer token，必须与读取 token 不同 |
| `VASTPLAN_ARTIFACT_BUNDLE_TOKEN` | 离线 Bundle 导出 Bearer token，必须与读取、发布 token 都不同 |
| `VASTPLAN_ARTIFACT_MIGRATION_STATE` | 仓库 volume 之外的私有 `0600` 迁移状态文件 |

令牌、私钥和信任文档不通过插件 API 返回，也不得写入日志、状态输出或普通设置。生产部署应以 Secret 文件或受控密钥注入提供这些值，并仅将需要的变量列入该第一方插件的环境白名单。

本地开发的稳定 `local-testing` 私钥由编排器保存在仓库目录之外的私有 `secrets/` 中，**不注入本插件**。插件只获得 testing-only 信任文档、TLS 材料和分离的读写 token。该自动生成行为不适用于生产环境。

## HTTP 协议

该服务强制 TLS：

- `POST /v1/artifacts`：使用发布 token 上传 multipart `attestation` 与 `package`；按部署策略可同时上传 `provenance`、`provenance-verification` 和 `security-admission`；
- `GET /v1/artifacts/{pluginId}/{version}/{channel}/package`：使用读取 token 下载包；
- `GET /v1/artifacts/{pluginId}/{version}/{channel}/attestation`：使用读取 token 下载已验证证明。
- `GET /v1/artifacts/{pluginId}/{version}/{channel}/security-admission`：使用读取 token 下载已验证的不可变安全准入记录；未配置或非必需 channel 可以不存在。
- `GET /v1/catalog/artifacts`：使用读取 token 分页查询目录；支持 `pluginId`、`pluginPrefix`、`namespace`、`publisher`、`version`、`channel`、`target`、`page` 和 `pageSize`；
- `GET /v1/catalog/journal`：使用读取 token 按 `afterRevision` 与 `limit` 增量读取发布事件。
- `POST /v1/catalog/resolve`：使用读取 token，根据根约束、目标内核/平台、channel、发布者和 Catalog revision 生成精确 `ArtifactLock v1`；
- `POST /v1/catalog/bundles`：使用独立 Bundle token 提交一份已校验锁，下载包含锁、信任快照、精确包与证明的确定性 `tar.gz`。
- `POST /v1/catalog/bundles/import`：使用发布 token 流式上传 Bundle；服务先在仓库外的私有临时目录解包，再将每个对象送回相同的签名、摘要、包内清单和不可变发布强制点。部分导入只会留下已验证的幂等对象，不会激活任何锁或部署。

包体默认上限为 256 MiB，证明上限为 2 MiB。未授权、明文请求、超限或不可信制品均 fail-closed。小载荷管理操作已经 Node Portal Kernel BFF；制品包与 Bundle 大字节仍使用独立 TLS 入口。通用设置/凭证句柄与签名种子 Bundle 尚未接入，所以 `artifact-server` 子命令仍保留为兼容启动路径，不能据此删除自举能力。

清单从 `0.14.0` 起声明 `artifact-data` Data Plane Service。配置 `apiExposure` 后，仓库实例通过受委托插件调用登记/续租 Endpoint Lease；API Exposure 控制面只向精确的 `installDataPlaneTicket` operation 安装 Ticket。公开客户端先请求 `/api/d/{routeKey}/ticket`，再以 `vp_ticket` 访问仓库；中间件本地单次消费 Ticket、移除 query，并只在进程内投影私有读取凭证。Ticket 不写日志、不写持久状态，也不会替代仓库的原有 TLS 与签名复验。

Resolver 请求示例：

```json
{
  "roots": [{ "pluginId": "cn.vastplan.product.example", "constraint": "^1.4" }],
  "target": "backend",
  "kernelVersion": "0.1.0",
  "platform": "linux/amd64",
  "allowedChannels": ["stable"],
  "allowedPublishers": ["vastplan"],
  "allowedPluginPrefixes": ["cn.vastplan"],
  "availableCapabilities": [{ "capability": "platform.settings", "version": "1.0.0" }],
  "snapshotRevision": 42
}
```

`snapshotRevision: 0` 或省略时，服务端在请求内原子锁定当前 revision，并把实际值写入返回锁。`allowedChannels` 的顺序是同版本 channel 的优先级；最终锁不保存 `latest` 或 URL。

Catalog 数据保存在仓库 volume 的 `catalog/` 下。发布流水账按单调 revision 使用原子事件文件，索引快照可从每个签名制品及流水账重建；启动时发现制品已成功落盘但事件缺失，会补写 `recovered` 事件。恢复路径只读取并验证 artifact metadata 与 attestation，不扫描全部大对象；实际读取仍由内核复验对象摘要。相同精确 ref、摘要和证明重传幂等，不增加 revision；受控测试 CLI 会先查 Catalog，避免重试产生不同证明。

平台工具能力同时提供 `status/capacity`、`listCatalog`、`listPublishJournal`、小载荷 `resolve`、`setLifecycle`、`putReferences/listReferences`、`gcPlan/gcStatus/gcQuarantine/gcSweep`，以及 `migrationStatus/prepareMigration/syncMigration/cutoverMigration/rollbackMigration/finalizeMigration/releaseMigration`。引用发布只接受宿主验证的租户与精确首方控制器身份；完整快照使用 generation、可选 TTL 和规范摘要，过期会令 GC fail-closed，且继续保护字节。完整快照可以包含由消费者从签名 Seed 等其他可信源取得、尚未复制进 Managed Catalog 的精确 ref+SHA；本仓已知 ref 仍必须摘要一致，已退休对象仍拒绝。未知对象只持久化为未来保护声明，GC 只保护本仓实际存在且精确 SHA 命中的对象。`bootstrap-inventory/<repositoryId>` 系统身份仍只能写匹配 ID 的 Seed/LKG 快照，且其清单由内核逐项从 Seed 重新验签。生命周期变更使用 Catalog revision CAS 和独立权限，`deprecated` 会进入锁提示，`yanked/revoked` 拒绝新的解析与交付。

stable 发布提供 `listPublications/submitPublication/approvePublication/rejectPublication/cancelPublication/getSupplyChainEvidence`。申请只能引用仓库中已验签、active 的 testing 制品，并绑定目标 stable ref、SHA-256、publisher、key ID、来源证明摘要、安全准入记录摘要和服务端有效期；批准人与提交人必须不同。独立审批人可驳回待批或已批申请，原提交人可撤销，人工终止原因必填；默认 168 小时后自动进入 `Expired`。最终 HTTPS stable 上传在物理写入前再次验签、收敛过期状态并要求精确匹配批准记录，成功后记录最终证明摘要。只有发布 token、只有 Portal 权限或只有批准记录都不能单独完成 stable 发布。崩溃后从已验签 Catalog 收敛 `Approved -> Published`，迁移观察期两卷审批状态不一致会冻结读取和发布。

生产 CI 使用 `pluginpackage -package <testing候选.tar.gz> -channel stable` 完成最终上传。CLI 要求与发布 token 分离的读取 token，先从远端重新验签同版本 testing 候选并比较 SHA、大小、publisher 与 key ID，再签署 stable 证明；自定义企业 CA 通过 `-remote-ca` 指定。该预检不替代仓库批准强制点。

插件包可在签名清单 `supplyChain.sbom` 中声明固定路径 `supply-chain/sbom.cdx.json`。内核接受 CycloneDX JSON 1.5/1.6，并在发布时校验文档大小、组件上限、插件 ID/版本、路径与摘要绑定；仓库默认对 `stable` 强制要求 SBOM，策略键为 `supplyChain.requiredSBOMChannels`。Catalog 列表只显示是否已绑定，避免列表读取大包；供应链证据 Overlay 会按需读取完整包和证明并返回实际组件数、serial number、规范版本和复验摘要。构建来源证明保持包外 SLSA/in-toto sidecar，不能以内嵌自报字段代替。

Python 制品另外声明 `supplyChain.pythonLock`，固定绑定 PEP 751 `supply-chain/pylock.toml` 1.0。内核在发布和读取时复核全部本地 wheel 的路径、大小和 SHA-256；Catalog 显示锁是否闭合，证据 Overlay 按需返回解释器范围、生成器、包数和 wheel 数。Node Agent 只从这些已验证字节离线物化依赖，不访问远端索引。

包外来源证明现由 DSSE/in-toto SLSA 原文与外部 Verifier 签发的 Verification Record 组成。两者通过远端 multipart 与制品一起提交，但不改变 tar 字节；仓库在物理发布前用部署信任文档复验 Provider key、Record 有效期、策略、原文摘要和 tar subject，Node Agent 下载时再次执行同一检查。Catalog 只保存经过复验的 builder/build type/Provider/policy 与摘要，证据 Overlay 按需读取 sidecar 复核；原始证明不会进入普通列表。testing→stable 审批绑定两份 sidecar 摘要，CLI 原样复用 testing 证据。离线 Bundle、File Volume 迁移、恢复与 GC 均携带 sidecar，不能形成缺证据的旁路。参考外部静态 Provider 为 `engineering/tools/provenanceverify`；GitHub/Sigstore 或企业 CA Provider 只需输出同一 Record。

漏洞与许可证准入使用独立 `security-admission.json`。记录由外部 Security Assessment Provider 生成，统一绑定 tar SHA-256、签名清单中的 SBOM SHA-256、scanner/database revision、策略、有效期、风险计数和两份原始报告摘要；内核不绑定 Trivy、OSV、Grype 或 ScanCode。部署信任文档的 `assessment` 段按 channel/publisher/plugin prefix 选择可信 Provider、scanner 和阈值。记录 decision 为 fail、过期、超阈值、签名无效或 tar/SBOM 绑定漂移时，仓库发布与 Node 安装均拒绝。当前 0.26.0 完成不可变准入记录；只追加复扫状态按 ADR-0138 下一阶段接入。

发布准入在仓库 leader 的同一串行临界区内执行：全仓上限与所有匹配规则累积生效，任一超限即在物理写入前拒绝，且不会自动运行 GC。已隔离/清扫对象不再占活动配额，因此可以发布替代版本；隔离字节仍计入实际存储容量，直到 sweep 后才计入 reclaimed。`capacity` 只聚合已验证 Catalog 与持久 GC 元数据，分别返回活动、隔离、已清扫、已回收和按 namespace/publisher/channel 的活动 bucket；对象字节不包含 Catalog/证明等小型元数据开销。降低配置到当前用量以下不会阻止仓库启动，但会把对应 quota 标为 `exceeded` 并冻结后续新增发布，便于先治理再恢复。

GC 只把已显式 `yanked/revoked`、无精确引用且不在既有 retirement 状态的制品列为候选，绝不隐式下架 `active/deprecated`。plan 不写状态；quarantine 重新计算 plan 身份并要求至少一个健康 Seed/LKG、所有租约源未过期、仓库迁移完全结束，随后逐项原子移出活动命名空间。隔离宽限期至少 24 小时；sweep 再次复核引用健康、生命周期和精确保护后才删除。中断的 quarantining/sweeping 在启动时幂等恢复，Catalog 只允许 GC 状态中精确记录的缺失制品继续保留历史。已进入 retirement 的 ref 禁止重发、重新激活或被新快照引用。迁移采用可重试阶段命令；观察期的发布、生命周期和引用快照都先镜像后提交活动卷，失败可回滚；GC 在迁移未完全结束时冻结。物理 path/handle 不返回 Portal，Bundle 大字节只走 HTTPS，不穿过协议总线。

## 验证

`core/kernels/backend/pluginservice/remote_test.go` 通过该共享 HTTP 传输层覆盖 TLS、读写 token、签名发布与再次读取；`engineering/tools/platformdev/artifact_repository_test.go` 覆盖持久测试身份复用、组合信任、Seed/远端源顺序、私钥不注入和目录权限。仓库插件本身只负责配置、进程生命周期和对外贡献。ADR-0049 与 ADR-0097 是该边界的权威决策记录。

## Portal 管理页

同一签名制品从 `/settings/artifacts` 进入统一 Workbench，并在受治理的“系统设置 → 制品仓库”三级导航下提供目录、容量/配额、引用快照、GC 和存储迁移五个 Collection 页面。目录经固定 BFF/TypeScript SDK 路由按 plugin prefix、namespace、publisher、channel、target、lifecycle 与分页查询，不把仓库读令牌交给浏览器；概览与集合查询独立，筛选/翻页不会重复拉取容量。目录行通过受治理的 Workbench Form 以当前 Catalog revision CAS 变更生命周期，原因必填，替代插件与 SemVer 约束只能同时用于 `deprecated`，`revoked` 不提供再次编辑入口。GC 页面展示阻断项、候选与 retirement 记录，隔离前重新生成 plan，quarantine/sweep 继续受 CSRF 和独立 `platform.artifacts.gc` 角色保护。

存储迁移页面直接投影后端阶段状态，只在记录操作区提供 `sync/cutover/rollback/finalize/release`，准备与切换参数进入 Workbench Form；所有按钮仍受 `platform.artifacts.migrate`、CSRF 和后端状态机三重校验。页面不接收 mount path，只显示稳定 Provider/Volume ID、摘要、计数、观察期与脱敏错误。目录中的 testing 制品可提交 stable 发布申请，独立“发布审批”页面提供批准、驳回和撤销，并显示有效期及可选终态审计列；按钮可见性只用于体验，身份分离和状态转移仍由后端强制。目录新增 SBOM、Python 标准锁、来源证明和安全准入状态；供应链证据 Overlay 每次通过可信仓库复验证明、可选 SBOM、Python 锁与安全准入记录，只显示 SHA、publisher、key ID、证明摘要、SBOM 摘要/组件计数、Python 包/wheel 计数、扫描器/数据库 revision、风险计数和审批轨迹。现有页面和 API 均不返回令牌、信任根、Provider endpoint、原始签名、SBOM 正文、锁正文、扫描器原始报告或制品正文。
