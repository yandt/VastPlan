# 本地文件制品存储 Provider

插件 ID：`cn.vastplan.platform.artifacts.storage.file`  
能力：`tool.package/platform.artifacts.storage.file`  
当前制品版本：`0.1.0`

## 职责

本插件只负责制品 volume 的 `probe` 与幂等 `provision`。它不接收发布包、不执行制品验签，也不代理仓库的对象读写。仓库使用 Provider 供给的本地挂载数据面，详细边界见 [ADR-0091](../decisions/ADR-0091-制品存储Provider供给边界.md)。

## 安全边界

- Provider root 必须是规范绝对路径、真实目录且权限不宽于 `0700`；
- volume ID 只能使用小写分级标识，不能包含 `/`、`..` 或大写字符；
- volume 只会创建在 root 的直接子级，权限固定为 `0700`；
- 返回 handle 不包含真实路径；`mountPath` 只供可信部署适配器，不能经普通 Portal API 暴露；
- `provision` 不删除、不覆盖已有制品数据；回收需要未来独立审批流程。

## 运行配置

| 环境变量 | 含义 |
|---|---|
| `VASTPLAN_ARTIFACT_FILE_PROVIDER_ROOT` | 部署层授予的私有存储根目录 |
| 插件配置 `volumeId` | 启动时预供给的 volume ID；默认 `repository.primary` |

这两个值属于节点部署适配，不是租户业务设置。S3/OCI 等 Provider 的访问凭证必须使用 ADR-0090 的托管凭证，不得复用本插件环境变量约定。

## API

| 操作 | 含义 |
|---|---|
| `probe(volumeId)` | 校验 root 与 volume ID 是否满足安全基线 |
| `provision(volumeId)` | 幂等创建私有 volume，返回非敏感 `Volume` |
