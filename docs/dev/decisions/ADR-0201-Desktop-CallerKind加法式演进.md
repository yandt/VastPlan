# ADR-0201 Desktop CallerKind 加法式演进

- 状态：已接受
- 日期：2026-08-08
- 关联：[ADR-0196 Desktop 内核规范 ID 改名](ADR-0196-Desktop内核规范ID改名.md)、[插件契约与协议](../architecture/插件契约与协议.md)

## 背景

Desktop 内核的规范 ID 已由 `runner` 改为 `desktop`，但不可变 wire 契约仍只有 `CALLER_KIND_RUNNER = 5`。直接把枚举项改名会改变 protobuf JSON 名称、生成代码符号和反射描述符；复用编号也会让新旧端对同一字节产生不同语义。

## 决策

1. 在 `CallerKind` 中新增 `CALLER_KIND_DESKTOP = 6`。这是 v1 契约的加法式演进，不改变任何既有字段或编号。
2. `CALLER_KIND_RUNNER = 5` 保留并标记 `deprecated`。编号 5 永不删除、重编号或复用。
3. Desktop 内核及后续新增的首方 Desktop 写端只发送 `CALLER_KIND_DESKTOP = 6`。
4. 迁移期内，授权策略、Interaction Broker、Database Runtime、API Exposure 等读取端同时接受 5 和 6，并把两者解释为同一 Desktop 信任类别。兼容读取不得扩张原有权限条件。
5. 兼容矩阵同时冻结 `RUNNER = 5` 与 `DESKTOP = 6`；生成的 Go/Python 契约产物必须与 proto 同次提交。

## 影响

- 新 Desktop 请求在日志、策略和反射接口中使用准确的 `DESKTOP` 名称。
- 尚未升级的写端仍可发送 5；升级后的读取端不会把它误判为未知调用方。
- 只认识 0-5 的旧读取端会把 6 当作未知枚举值，因此发布顺序必须先升级读取端，再启用新的 Desktop 写端。首方仓库在同一版本中完成这两部分，跨版本滚动升级仍遵循该顺序。
- 删除 5 的兼容窗口不在本 ADR 范围；即使未来停止接受 5，其 wire 编号仍必须永久保留。
