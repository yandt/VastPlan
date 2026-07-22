# 插件文档（Plugins）

> 本目录存放**具体插件**的文档——即在 VastPlan 平台之上开发的一个个第一方插件（审计、可观测、用户系统扩展、Studio 模块等）各自的设计与使用说明。

**注意**：插件机制本身的**系统设计**（统一插件定义/清单、宿主协议、契约字段、扩展点模型、生命周期、部署编排）不在这里，而在架构文档：

- 插件如何声明、如何通信、携带什么数据 → 《[插件契约与协议](../architecture/插件契约与协议.md)》
- 微内核、扩展点、四内核、通信、插件服务与编排 → 《[系统架构](../architecture/系统架构.md)》
- 为什么这么设计 → 《[决策记录 ADR](../decisions/README.md)》

## 本目录规范

- 每个插件一个子目录或一篇文档：`<plugin-id>.md` 或 `<plugin-id>/`。
- 内容聚焦该插件自身：它贡献什么（对应清单 `contributes`）、依赖什么、配置项、使用说明、版本历史。
- 新增插件文档时在 [文档地图](../00-index.md) 的"插件"小节登记。

## 当前插件文档

- [cn.vastplan.python-hello](cn.vastplan.python-hello.md) —— Python SDK 与异构运行参考插件
- [cn.vastplan.foundation.security.bootstrap-policy](cn.vastplan.foundation.security.bootstrap-policy.md) —— 系统设置之前启动的首方权限基线
- [cn.vastplan.foundation.security.platform-admin-access-policy](cn.vastplan.foundation.security.platform-admin-access-policy.md) —— 平台管理角色访问策略
- [cn.vastplan.foundation.security.authorization-enforcer](cn.vastplan.foundation.security.authorization-enforcer.md) —— 每内核签名策略验证、缓存与最终强制
- [cn.vastplan.foundation.security.authorization-engine.native](cn.vastplan.foundation.security.authorization-engine.native.md) —— 默认 Go authorization.engine.v1 与 Decision Proof Provider
- [cn.vastplan.foundation.data.relational.runtime](cn.vastplan.foundation.data.relational.runtime.md) —— Database Runtime wire 契约、Provider SPI 与可信数据面边界
- [cn.vastplan.platform.configuration.global-settings](cn.vastplan.platform.configuration.global-settings.md) —— 租户级版本化全局设置
- [cn.vastplan.platform.security.credentials](cn.vastplan.platform.security.credentials.md) —— Vault Transit 凭证元数据与轮换
- [cn.vastplan.platform.data.relational.connection-manager](cn.vastplan.platform.data.relational.connection-manager.md) —— 数据库连接定义与可信探测
- [cn.vastplan.platform.artifacts.repository](cn.vastplan.platform.artifacts.repository.md) —— HTTPS 制品仓库与状态能力
- [cn.vastplan.platform.artifacts.storage.file](cn.vastplan.platform.artifacts.storage.file.md) —— 本地文件制品 volume 供给 Provider
- [cn.vastplan.platform.integration.api-exposure](cn.vastplan.platform.integration.api-exposure.md) —— API Contract、稳定 Route Key、Gateway Catalog 与独立数据面租约治理
- [cn.vastplan.platform.infrastructure.deployment-manager](cn.vastplan.platform.infrastructure.deployment-manager.md) —— 节点计划、首次引导审批与可信执行桥
- [cn.vastplan.platform.configuration.portal-composer](cn.vastplan.platform.configuration.portal-composer.md) —— Portal 分域治理、不可变 Activation 与前端制品引用保护
- [cn.vastplan.platform.security.authorization-policy](cn.vastplan.platform.security.authorization-policy.md) —— 在线 Role/Binding、撤权与签名 Policy Snapshot 真相源
- [cn.vastplan.platform.configuration.role-management](cn.vastplan.platform.configuration.role-management.md) —— 权限目录、角色、主体绑定与审计 Workbench
