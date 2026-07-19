# 全局设置基础插件

插件 ID：`cn.vastplan.platform.configuration.global-settings`
状态：已实现（首个基础服务）
能力：`tool.package/platform.settings`
当前制品版本：`0.2.0`

## 边界

该插件负责租户隔离的全局设置读写、版本前置条件和变更游标；不负责凭证明文、数据库连接或业务插件私有状态。权限由先于它启动的 `bootstrap-policy` 强制：只有 system 或直接登录的管理员可写，首方 foundation/platform 插件仅可读。

插件以 `leader + leader-owned + cluster + leader` 运行。内核负责 leader fencing、能力租约和故障重调度；状态卷的持久化、备份与故障域由部署适配器负责，内核不会复制插件私有状态。

## 部署配置

设置状态文件必须由同一 service unit 的 `config` 提供，键为 `platform.settings.stateFile`，值为非空 JSON 字符串，例如：

```json
{
  "config": {
    "platform.settings.stateFile": "/var/lib/vastplan/settings/state.json"
  }
}
```

插件在首次调用时通过认证的 `kernel.config.get` 读取该值，不读取环境变量、请求 payload 或其他插件配置。文件以原子替换写入、权限为 `0600`；生产部署应将其置于持久卷，并确保 leader 故障接管时能够访问同一持久状态。

## API

所有调用都必须携带 `CallContext.tenant_id`；不同 tenant 的相同 key 完全隔离。

| 操作 | 输入 | 结果 |
|---|---|---|
| `get` | `key` | 值、版本、更新时间 |
| `list` | 可选 `prefix` | 按 key 排序的设置列表 |
| `put` | `key`、任意 JSON `value`、可选 `ifVersion` | 新版本与更新时间 |
| `delete` | `key`、可选 `ifVersion` | 删除后的全局版本 |
| `changesSince` | `version` | 后续变更记录 |

`ifVersion: 0` 表示仅在 key 尚不存在时写入。已有值的版本不匹配会明确拒绝；变更游标早于插件保留窗口时会拒绝，调用方需重新 `list` 建立快照。

## 验证

```bash
go test ./extensions/plugins/cn.vastplan.platform.configuration.global-settings/...
go run ./engineering/tools/pluginpackage \
  -source extensions/plugins/cn.vastplan.platform.configuration.global-settings \
  -backend-bin <global-settings-binary> \
  -out /tmp/global-settings.tar.gz
```

制品安装、leader 选举与启动依赖仍由 Node Agent 和 Deployment Controller 按签名 Manifest 执行。

## Portal 管理页

同一签名制品提供 `/settings/global` 页面。页面经强类型平台 BFF 列表、写入和带版本删除设置；JSON 值保存前在浏览器和后端分别校验。页面不允许保存凭证，权限与远端寻址边界见《[平台管理中心](../architecture/平台管理中心.md)》。
