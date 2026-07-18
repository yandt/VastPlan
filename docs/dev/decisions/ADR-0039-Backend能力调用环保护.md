# ADR-0039 Backend 能力调用环保护

- 状态：已采纳
- 日期：2026-07-16
- 关联：[ADR-0004 插件运行形态](ADR-0004-插件运行形态.md)、[ADR-0021 权限判定强制点](ADR-0021-权限判定强制点.md)、[ADR-0034 Backend 协议资源边界](ADR-0034-Backend协议资源边界.md)

## 背景

插件可通过 `Host.Call` 按 capability 调用内核服务或其他插件。调用目标之间没有编译期依赖，但运行时仍可能形成 `A → B → A`。只依赖 deadline 能最终终止请求，却会在超时前持续占用并发槽、pending 表和插件 goroutine；并发流量下会放大为级联拥塞。

## 决策

1. 在公共 `CallContext` 追加 `call_path` 字段，元素使用 `extension_point/capability#operation` 的稳定目标标识。字段只新增、不改已有编号。
2. `Host.Invoke` 是唯一维护边界：每次公开调用先克隆上下文，再检查目标是否已经出现在路径中，随后追加目标。重复目标返回应用层错误 `call.cycle_detected`。
3. `core/shared/go/protocollimit.Limits` 增加 `MaxCallDepth`，默认 16。到达上限时返回 `call.depth_exceeded`，防止全部目标不同的过长调用链。
4. 第一方 SDK 从处理器 context 继承宿主下发的路径，`Host.Call` 不接受处理器自行缩短路径。跨服务 addressing 原样传递同一 `CallContext`。
5. deadline 继续作为连接故障、非协作实现和其他异常的最终收敛边界，但不再承担正常调用环检测。

## 备选方案

- **只依赖 deadline**：实现简单，但错误发现太晚，且会把逻辑环表现成资源耗尽。拒绝。
- **禁止插件调用插件**：能消除一类环，但同时破坏 capability 组合与位置透明。拒绝。
- **只记录整数深度**：能限制递归，不能给出具体环路，也会让短环重复执行多次。拒绝。
- **由每个插件自行检测**：插件无法看到完整跨进程、跨服务路径，且实现会漂移。拒绝。

## 影响

- 正面：调用环在第二次进入同一目标前 fail-fast，错误码和诊断路径稳定；调用链资源有明确上限。
- 代价：`CallContext` 每跳增加一个短字符串，占用 metadata 预算；合法的深层组合需要显式提高部署限额并通过容量测试。
- 约束：插件处理器应把收到的 `ctx` 和 `CallContext` 传给 SDK；自行构造裸协议的非协作插件仍由 deadline、并发和 pending 限额兜底。
