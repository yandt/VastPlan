# ADR-0193 第一方插件边界与 Capability 契约版本

- 状态：已采纳
- 日期：2026-08-05
- 关联：[ADR-0017](ADR-0017-版本定义与兼容性机制.md)、[ADR-0152](ADR-0152-插件边界门禁与权限策略物理收敛.md)、[ADR-0192](ADR-0192-插件最小升级与兼容影响解析.md)

## 背景

第一方能力已经同时包含普通运行插件、全栈平台插件、按需前端 Library 和同一运行制品内的 Provider 模块。若只按目录数量治理，会把产品入口、签名制品和物理进程混为一谈；若运行时能力依赖继续用提供者插件 SemVer 匹配，又会把实现发布版本误当成接口兼容承诺。

## 决策

1. 第一方制品必须在边界清单中归类为 `runtime-plugin`、`signed-library` 或 `bundle`；产品能力包只存在于 Catalog/Planner 投影，不伪装成新的运行插件。
2. 独立制品继续满足 ADR-0152 的真实边界门禁。没有独立发布、配置、状态、外部适配、隔离、集群生命周期或按需下载边界的实现收进现有制品内部模块。
3. 数据库产品只暴露一个 `platform.database` 能力包；`connection-manager` 管理面和 `relational.runtime` 数据面保留两个制品。PostgreSQL、MySQL、Record Store 与 SQL Shared State 是 Runtime 内部模块，不增加制品和进程。
4. `runtime.provides` 必须声明精确 `contractVersion`；`runtime.requires` 使用 `contractRange` 声明可接受范围。旧 `runtime.requires.version` 删除，不保留兼容解释。
5. 插件 SemVer 只用于制品解析和回滚；Capability Contract Version 用于组合、激活和寻址；状态或 DataModel Schema Version 只用于持久数据迁移。
6. Catalog、Planner、Deployment、Node Agent 和全局能力目录分别携带 `ArtifactIdentity`、`ContractIdentity` 与公共接口 fingerprint。契约匹配成功后仍以精确制品摘要锁定一个 Generation。
7. 破坏性接口修改提升契约主版本和插件主版本；只增加兼容能力提升契约次版本；实现修复可以只提升插件版本。公共接口结构比较是发布门禁证据，不独自决定语义兼容。
8. 当前仍处于开发阶段，全部第一方 Manifest、Profile、Catalog 与测试夹具一次迁移到新字段，不建立旧字段分支。

## 影响

- 插件实现可独立发补丁而不迫使消费者修改依赖范围；
- 一个插件可以提供多个各自演进的 Capability；
- 用户管理产品能力，平台管理签名制品和运行拓扑；
- 本次先物理收敛数据库与种子链，其他第一方领域只建立全量审计和持续门禁。

## 实施记录

- 2026-08-05：Backend Platform Profile 新增强类型 `productCapabilities` 投影。`platform.database` 只暴露 Connection Manager 作为产品入口，同时绑定 Connection Manager 与 Database Runtime 两个既有签名制品；PostgreSQL、MySQL、Record Store 和 SQL Shared State 不进入产品可选列表。Profile 校验拒绝未知制品、重复归属、重复成员和不属于能力包的入口，架构门禁持续对照第一方边界清单。
