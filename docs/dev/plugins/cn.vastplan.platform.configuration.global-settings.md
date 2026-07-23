# 全局设置基础插件

插件 ID：`cn.vastplan.platform.configuration.global-settings`
状态：已实现（首个基础服务）
能力：`tool.package/platform.settings`
当前制品版本：`0.8.0`

## 边界

该插件负责租户隔离的全局设置读写、版本前置条件和变更游标；不负责凭证明文、数据库连接或业务插件私有状态。权限由先于它启动的 `bootstrap-policy` 强制：只有 system 或直接登录的管理员可写，首方 foundation/platform 插件仅可读。

插件以 `active-active + external-shared + cluster + queue` 运行。每个 tenant 的完整设置、变更窗口和业务 revision 聚合为 Shared State 中的单个 CAS 文档；多个实例可以并行读取，写入使用 Store revision 防止 stale writer 覆盖。业务 API 仍只暴露设置 version，不泄漏 NATS revision。

## 部署配置

插件不再接受 `platform.settings.stateFile`，也不直接连接 NATS 或数据库。签名清单只申请 `kernel.state.shared.get/create/update`；宿主从验签 RuntimeIdentity 与可信 tenant 构造命名空间。生产使用 NATS KV Provider，本地 File Provider 只可由 Backend 开发入口注入，Provider 不可用时禁止自动回退。

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

制品安装、副本调度与启动依赖仍由 Node Agent 和 Deployment Controller 按签名 Manifest 执行。状态格式已从旧插件私有文件切换为 `cn.vastplan.platform.settings.shared@1`；当前开发阶段不迁移旧测试文件，生产形成历史数据后必须使用独立迁移事务。

## Portal 管理页

同一签名制品提供 `/settings/global` 页面。0.3 已迁移到 UI Workbench：集合筛选、分页、行操作、新增/编辑 Drawer、脏数据关闭确认、JSON 异步校验、字段错误和成功刷新均由基础工作流统一处理；功能插件只提供强类型 BFF loader/submit/delete。编辑继续携带 `ifVersion`，并发覆盖会由服务端拒绝。页面明确只接受非敏感 JSON，不提供密码、令牌或密钥输入；权限与远端寻址边界见《[平台管理中心](../architecture/平台管理中心.md)》。
