# Graphify 主图维护

> 状态：v1.0｜最后更新：2026-07-18
> 本文是 VastPlan Graphify 收录边界、更新命令和质量判断的单一真相源。

## 1. 主图用途

`graphify-out/graph.json` 是面向以下任务的本地派生主图：

- 理解产品架构边界和设计决策；
- 定位跨模块依赖、调用链和协议边界；
- 从代码跳转到对应 ADR、架构文档和插件文档。

主图不是测试覆盖图，也不能替代源代码、安全审计和运行时验证。Graphify 查询结果用于缩小核查范围；`INFERRED` 关系只能作为线索。

## 2. 收录边界

主图保留：

- `core/`、`extensions/`、`contracts/` 中的产品实现；
- `docs/dev/` 中的架构、ADR、指南和插件文档；
- 外部依赖或未解析符号中，仍被保留关系引用的节点；
- Protobuf 生成代码中的导出契约声明，例如 `CallContext`、`CallTarget` 和服务接口。

主图排除：

- 单元测试、架构适应度测试、E2E 和故障夹具；
- Protobuf 的 `Reset`、`ProtoReflect`、`GetXxx` 等生成样板；
- `graphify-out/memory/` 查询记忆，避免历史结论反向证明自身；
- `AGENTS.md`、`CLAUDE.md` 等 Agent 指令回声；
- 未被任何保留关系引用的孤立外部节点。

`.graphifyignore` 负责扫描阶段降噪；`engineering/tools/graphify-primary.py` 是最终强制点，并为节点和关系写入 `source_scope`。关系中的 `source` 到 `target` 保留真实方向，但主图仍以无向图加载，保证自然语言广度查询同时看到入边和出边。`graphify path` 与 `graphify explain` 会按已保存端点显示方向。

## 3. 更新方式

在项目根目录执行：

```bash
engineering/tools/update-graphify-primary.sh
```

该命令依次完成增量提取、主图压缩、社区重聚类、报告/HTML 更新和最终校验。只需重新压缩已有图时：

```bash
engineering/tools/update-graphify-primary.sh --optimize-only
```

不要直接把 `graphify-out/` 提交到 Git；它始终是可重建的本地派生数据。

## 4. 查询纪律

1. 使用 `graphify query` 做架构定位和候选集合发现。
2. 使用 `graphify explain` 或精确节点 ID 消除同名符号歧义。
3. 使用 `graphify path` 追踪跨模块关系，并关注箭头方向与置信度。
4. 对关键结论回到源文件、测试和设计文档验证。
5. 安全结论不得仅由“图中没有路径”推出；图谱不证明代码中不存在关系或漏洞。

## 5. 质量门槛

更新后至少满足：

- 无测试、查询记忆或 Agent 指令节点；
- 无悬空关系端点；
- 所有节点和关系均有 `source_scope`；
- 所有关系均声明 `direction_semantics=source-to-target`；
- `CallContext` 等关键协议声明仍可查询；
- Portal 双输入和插件安装到 Router 注册等典型链路仍能定位。
