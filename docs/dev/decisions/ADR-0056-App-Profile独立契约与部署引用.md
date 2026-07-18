# ADR-0056 App Profile 独立契约与部署引用

- 状态：已采纳
- 日期：2026-07-18
- 关联：[ADR-0011 组合是通用内核能力](ADR-0011-组合是通用内核能力.md)、[ADR-0012 Runner 内核运行模型](ADR-0012-APP内核运行模型.md)、[ADR-0014 四内核结构](ADR-0014-四内核结构.md)

## 背景

`deployment/v2` 当前只描述由控制器放置到服务节点的 `serviceUnit`。Runner App/Profile 是预构建、签名并由客户端领取的制品声明，不具备服务副本、节点亲和、自动伸缩或 Node Agent 启停语义。把 `app` 直接加入 `units` 联合类型会让调度器误把客户端 Profile 当作后端服务处理。

## 决策

1. 新增独立的 `contracts/schemas/app/v1` Runner Profile 契约，声明 tenant、revision、目标 OS/架构、`self-update` 分发、领取对象和精确插件引用。
2. `deployment/v2` 通过顶层 `app_profiles` 引用 Profile 的 `id + revision + sha256 digest`，不把 Profile 放进 `units`。
3. 相同 Profile ID 在一份 deployment 中只能出现一次；digest 将期望态绑定到不可变 Profile 内容，避免同 revision 被替换。
4. 服务调度器继续只遍历 `units`。Profile 发布、构建、签名和领取由独立 App Profile 控制面处理。
5. Runner Profile 领取仍以受验证 Runner identity、tenant 与 `assignedTo` 为强制边界。

## 备选方案

- **把 app 作为 `units` 的 oneOf 分支**：统一外观，但会污染服务调度和 Node Agent 语义；拒绝。
- **完全脱离 deployment**：实现简单，但失去统一 revision 下服务与客户端组合的期望态关联；拒绝。
- **在 deployment 内嵌完整 Profile**：会复制 Profile Schema，并增大每次 deployment 变更；拒绝。

## 影响

服务调度路径保持兼容；控制面可以逐步增加 Profile resolver、构建和签名流水线。Mobile 后续可新增自己的 App Profile 契约或在兼容的 major 版本中扩展，不能借用 Runner 的后台执行语义。
