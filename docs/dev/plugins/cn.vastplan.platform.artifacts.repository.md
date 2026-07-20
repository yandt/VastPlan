# 制品仓库基础插件

插件 ID：`cn.vastplan.platform.artifacts.repository`
当前制品版本：`0.4.0`

仓库的数据面由存储 Provider 在配置/启动阶段供给。当前开发组合使用 `cn.vastplan.platform.artifacts.storage.file`，仓库状态 API 会返回实际 `storageProvider`；对象发布和读取仍直接使用已供给的本地数据面，不逐对象调用 Provider。设计原因见 [ADR-0091](../decisions/ADR-0091-制品存储Provider供给边界.md)。

能力：`tool.package/platform.artifacts.repository`

## 边界

该第一方基础插件运行 HTTPS 制品发布与读取服务，负责 HTTP 传输、读写令牌分离、可重建 Catalog、单调 Publish Journal 和运行状态查询。对象存储与 OCI 通过供给 Provider 增加；依赖解析、复制、审批和市场 API 仍在仓库领域扩展。

插件**不拥有信任解释权**：每次发布都交给内核 `SignedRepository` 校验清单、SHA-256、发布者证明、撤销状态和不可变版本；每次读取也只转发内核已验证的包与原始证明。Node Agent 对从任何来源取得的 `Envelope` 仍会在自己的强制点再次验证，不能把本服务的 HTTPS 或“已读取”当作可信标志。

当前以 `leader / leader-owned / cluster` 运行。集群成员可通过 `platform.artifacts.repository` 查询状态，数据复制与多活对象存储尚未在本版本提供，不能把多个实例指向不同本地目录后宣称高可用。

本地平台开发组合已经把它与临时 Seed 仓库分离：Seed 只负责本次启动的基础制品，本插件使用 `.vastplan/dev-platform/repositories/testing/volumes/repository.primary` 作为跨普通重启保留的测试数据面。Node Agent 将 Seed 作为优先 bootstrap source，将本插件的 HTTPS 端点作为普通远端 source；Node Agent 通过本次运行的组合信任快照复验两者，而本插件自身只加载 testing-only 信任文档，不接受临时 Seed 身份发布的制品。

## 运行配置

签名清单声明的非敏感插件配置通过调用方隔离的启动快照注入：

| 字段 | 含义 |
|---|---|
| `listen` | HTTPS 监听地址；默认 `127.0.0.1:8443` |
| `storageProvider` | 已供给当前数据面的 Provider 能力 ID |

部署适配器仍需向该第一方进程提供以下受控挂载/秘密；它们不属于普通插件设置：

| 变量 | 含义 |
|---|---|
| `VASTPLAN_ARTIFACT_REPOSITORY` | 不可变本地制品存储根目录 |
| `VASTPLAN_ARTIFACT_TRUST` | 发布者 Ed25519 信任文档 |
| `VASTPLAN_ARTIFACT_TLS_CERT` / `VASTPLAN_ARTIFACT_TLS_KEY` | TLS 证书与私钥 PEM |
| `VASTPLAN_ARTIFACT_READ_TOKEN` | 制品读取 Bearer token |
| `VASTPLAN_ARTIFACT_PUBLISH_TOKEN` | 制品发布 Bearer token，必须与读取 token 不同 |

令牌、私钥和信任文档不通过插件 API 返回，也不得写入日志、状态输出或普通设置。生产部署应以 Secret 文件或受控密钥注入提供这些值，并仅将需要的变量列入该第一方插件的环境白名单。

本地开发的稳定 `local-testing` 私钥由编排器保存在仓库目录之外的私有 `secrets/` 中，**不注入本插件**。插件只获得 testing-only 信任文档、TLS 材料和分离的读写 token。该自动生成行为不适用于生产环境。

## HTTP 协议

该服务强制 TLS：

- `POST /v1/artifacts`：使用发布 token 上传 multipart `attestation` 与 `package`；
- `GET /v1/artifacts/{pluginId}/{version}/{channel}/package`：使用读取 token 下载包；
- `GET /v1/artifacts/{pluginId}/{version}/{channel}/attestation`：使用读取 token 下载已验证证明。
- `GET /v1/catalog/artifacts`：使用读取 token 分页查询目录；支持 `pluginId`、`pluginPrefix`、`namespace`、`publisher`、`version`、`channel`、`target`、`page` 和 `pageSize`；
- `GET /v1/catalog/journal`：使用读取 token 按 `afterRevision` 与 `limit` 增量读取发布事件。

包体默认上限为 256 MiB，证明上限为 2 MiB。未授权、明文请求、超限或不可信制品均 fail-closed。当前服务是独立 TLS 入口；平台 Edge API Route、设置/凭证句柄与签名种子 Bundle 尚未接入，所以 `artifact-server` 子命令仍保留为兼容启动路径，不能据此删除自举能力。

Catalog 数据保存在仓库 volume 的 `catalog/` 下。发布流水账按单调 revision 使用原子事件文件，索引快照可从每个签名制品及流水账重建；启动时发现制品已成功落盘但事件缺失，会补写 `recovered` 事件。恢复路径只读取并验证 artifact metadata 与 attestation，不扫描全部大对象；实际读取仍由内核复验对象摘要。相同精确 ref、摘要和证明重传幂等，不增加 revision；受控测试 CLI 会先查 Catalog，避免重试产生不同证明。

平台工具能力同时提供 `status`、`listCatalog` 和 `listPublishJournal`。浏览器应经 Portal Edge 和能力授权调用工具，不得直接持有仓库读令牌。

## 验证

`core/kernels/backend/pluginservice/remote_test.go` 通过该共享 HTTP 传输层覆盖 TLS、读写 token、签名发布与再次读取；`engineering/tools/platformdev/artifact_repository_test.go` 覆盖持久测试身份复用、组合信任、Seed/远端源顺序、私钥不注入和目录权限。仓库插件本身只负责配置、进程生命周期和对外贡献。ADR-0049 与 ADR-0097 是该边界的权威决策记录。

## Portal 管理页

同一签名制品提供 `/settings/artifacts` 只读状态页。当前页面仍只显示真实就绪状态、监听地址和 Provider ID，不返回令牌、信任根、存储路径，也不复用仓库上传 API。Catalog/Journal 后端查询已就绪，目录、审批与供应链证明的 Workbench UI 将在独立管理契约封板后接入。
