# 插件市场 Platform 系统插件

`cn.vastplan.platform.artifacts.marketplace` 是平台级系统插件，不是内核能力，也不是 Artifact Repository 的附属页面。它拥有独立的发布、配置、权限和故障边界，通过 `platform.artifacts.marketplace` capability 向 Portal 提供多个市场来源的目录发现。

## 职责边界

- Marketplace：管理最多 32 个可自定义来源 URL、优先级和托管凭证引用，聚合受限目录查询，并标识哪些条目可由服务选择；
- Artifact Repository：保存、验签、索引和锁定本地可信制品；
- Deployment Manager：把安装请求转换为统一候选、审批、Generation 和回滚；
- Kernel：只负责可信加载、调用路由、权限强制和制品最终复验。

Portal 只能提交来源 ID，不能提交 URL、凭证、目标服务或 capability 路由。生产远端 URL 强制 HTTPS；显式 loopback HTTP 仅用于开发，运行时拒绝 HTTP 重定向。认证 material 通过可信宿主 lease 临时使用，不进入浏览器、配置投影或日志。

平台自己的 Catalog 使用 `vastplan://platform.artifacts.repository`，经 capability 路由访问。外部来源当前只开放发现；条目先经过受治理导入、验签和镜像进入 Artifact Repository 后，才会显示安装动作。这保证增加市场 URL 不会增加新的信任根，也不会绕过稳定制品身份规则。

服务自助安装页面从已发布 `ManagementTarget.resource` 固定目标服务，复用 Deployment Manager 的 `PluginInstallationCandidate`。审批方式由部署配置注入的 `approval.policy.v2` Provider/Profile 决定，Marketplace 本身不实现审批状态机。

实现决策见 [ADR-0191](../decisions/ADR-0191-统一插件安装意图与多入口生命周期控制.md)，完整运行链见《[服务部署控制台](../architecture/服务部署控制台.md)》。
