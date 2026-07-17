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

- [com.vastplan.python-hello](com.vastplan.python-hello.md) —— Python SDK 与异构运行参考插件
- [com.vastplan.foundation.security.bootstrap-policy](com.vastplan.foundation.security.bootstrap-policy.md) —— 系统设置之前启动的首方权限基线
