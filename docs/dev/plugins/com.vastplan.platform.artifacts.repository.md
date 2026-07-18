# 制品仓库基础插件

插件 ID：`com.vastplan.platform.artifacts.repository`

能力：`tool.package/platform.artifacts.repository`

## 边界

该第一方基础插件运行 HTTPS 制品发布与读取服务，负责 HTTP 传输、读写令牌分离和运行状态查询。它可以在不改动内核的情况下继续增加对象存储、OCI、索引、复制、审批和市场 API。

插件**不拥有信任解释权**：每次发布都交给内核 `SignedRepository` 校验清单、SHA-256、发布者证明、撤销状态和不可变版本；每次读取也只转发内核已验证的包与原始证明。Node Agent 对从任何来源取得的 `Envelope` 仍会在自己的强制点再次验证，不能把本服务的 HTTPS 或“已读取”当作可信标志。

当前以 `leader / leader-owned / cluster` 运行。集群成员可通过 `platform.artifacts.repository` 查询状态，数据复制与多活对象存储尚未在本版本提供，不能把多个实例指向不同本地目录后宣称高可用。

## 运行配置

第一方进程只能从部署方显式允许的受控环境取得以下配置：

| 变量 | 含义 |
|---|---|
| `VASTPLAN_ARTIFACT_LISTEN_ADDR` | HTTPS 监听地址；默认 `127.0.0.1:8443` |
| `VASTPLAN_ARTIFACT_REPOSITORY` | 不可变本地制品存储根目录 |
| `VASTPLAN_ARTIFACT_TRUST` | 发布者 Ed25519 信任文档 |
| `VASTPLAN_ARTIFACT_TLS_CERT` / `VASTPLAN_ARTIFACT_TLS_KEY` | TLS 证书与私钥 PEM |
| `VASTPLAN_ARTIFACT_READ_TOKEN` | 制品读取 Bearer token |
| `VASTPLAN_ARTIFACT_PUBLISH_TOKEN` | 制品发布 Bearer token，必须与读取 token 不同 |

令牌、私钥和信任文档不通过插件 API 返回，也不得写入日志、状态输出或普通设置。生产部署应以 Secret 文件或受控密钥注入提供这些值，并仅将需要的变量列入该第一方插件的环境白名单。

## HTTP 协议

该服务强制 TLS：

- `POST /v1/artifacts`：使用发布 token 上传 multipart `attestation` 与 `package`；
- `GET /v1/artifacts/{pluginId}/{version}/{channel}/package`：使用读取 token 下载包；
- `GET /v1/artifacts/{pluginId}/{version}/{channel}/attestation`：使用读取 token 下载已验证证明。

包体默认上限为 256 MiB，证明上限为 2 MiB。未授权、明文请求、超限或不可信制品均 fail-closed。当前服务是独立 TLS 入口；平台 Edge API Route、设置/凭证句柄与签名种子 Bundle 尚未接入，所以 `artifact-server` 子命令仍保留为兼容启动路径，不能据此删除自举能力。

## 验证

`core/kernels/backend/pluginservice/remote_test.go` 通过该共享 HTTP 传输层覆盖 TLS、读写 token、签名发布与再次读取；仓库插件本身只负责配置、进程生命周期和对外贡献。ADR-0049 是该边界的权威决策记录。
