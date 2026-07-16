# ADR-0041 Go 契约类型与 CAS 模板单一真源

- 状态：已采纳
- 日期：2026-07-16
- 关联：[ADR-0019 工程规范基线](ADR-0019-工程规范基线.md)、[ADR-0020 代码设计原则与复用策略](ADR-0020-代码设计原则与复用策略.md)、[ADR-0039 Backend 能力调用环保护](ADR-0039-Backend能力调用环保护.md)

## 背景

代码审计发现多组结构完全相同但分散定义的 Go 类型：插件状态身份存在于 Schema、SDK 和协议总线，部署资源请求在 v1/v2 各一份，soak 生产端和校验工具也各维护一份报告结构。控制面发布 DesiredState v1 与 Deployment v2 的 CAS 算法也逐行重复。它们当前字段一致，但修改任一份时编译器无法保证其他副本同步。

并非名字相似的类型都应合并：Node Agent 实际态使用 `format_version`，而插件清单使用 `formatVersion`；控制面迁移命令还比插件处理器请求多一个生命周期阶段。强行别名会改变外部 JSON 或混淆命令与负载语义。

## 决策

1. 插件 `StateIdentity` 与处理器 `MigrationRequest` 以 `schemas/plugin/v1` 为单一真源；Go SDK 使用类型别名。协议总线复用该身份类型，并将带阶段的宿主对象命名为 `MigrationCommand`，旧 `MigrationRequest` 仅保留兼容别名。
2. Node Agent 的 `PluginStateIdentity` 作为实际态 v2 的外部 JSON 形态继续保留，但所有跨边界转换必须经显式函数并有往返测试。
3. 部署 v1/v2 的 `ResourceList`、`ResourceRequirements` 上移到 `schemas/common/v1`，版本包只提供兼容别名；各版本 JSON Schema 仍独立冻结。
4. soak 报告结构与离线验收规则归入 `internal/soakreport`，E2E 生产端和工具消费同一类型。当前只整理代码，不恢复或执行 soak。
5. DesiredState 与 Deployment 的公开发布函数保持不变，同 revision 不可改写、不同 revision 通过 KV revision CAS 更新的算法收敛到 `controlplane.applyVersioned[T]`。

## 备选方案

- **继续保留副本并依赖评审同步**：没有编译期约束，字段和错误语义最终会漂移。拒绝。
- **所有相似类型全部别名**：会破坏既有 JSON 字段名并混淆迁移命令与处理器负载。拒绝。
- **用反射实现通用 CAS**：可减少代码，但丢失泛型类型检查，运行时才发现 revision/digest 缺失。拒绝。
- **把所有类型塞进一个 common 包**：形成无职责垃圾抽屉。只允许明确的 Schema 公共 DTO 与单一职责 internal 包。拒绝。

## 影响

- 正面：契约字段修改会沿别名触发编译检查；CAS 不变量和 soak 验收规则只有一份实现。
- 代价：版本包暴露的部分类型实际来自 common 包，定位定义时需要跟随别名；确有不同序列化语义的类型需要显式转换代码。
- 约束：后续审计应启用重复代码门禁；新增“同字段、同语义”类型前先查 Schema 和既有领域包。
