# VastPlan 文档地图

> 本文件是全部项目文档的唯一索引入口。任何人（包括 AI 协作者）查阅文档都从这里出发。
> `CLAUDE.md` 会引用本文件，确保每次会话都能顺着它找到最新文档。

## 一句话定位

VastPlan 是一套**基于 LLM 的通用 Agent 系统**，面向企业级客户，支持在线 Agent 开发、远程连接任务客户端在本地运行脚本/工作流。系统采用**微内核 + 全层扩展点**架构：内核只提供最小骨架，绝大多数功能（审计、可观测、用户系统扩展、Studio 可开发模块等）都是骨架之上的**第一方插件**。内核分**四套**——Backend / Frontend / Runner（桌面客户端执行器）/ Mobile（手机 Companion），规范 ID `backend/frontend/runner/mobile`；后端内核可灵活组合出 backend/workspace/rs 等服务。

> 插件当前**全部由本方开发（第一方、可信）**，暂不开放第三方。插件市场用于分发本方插件；第三方开放与相应的安全隔离是**未来议题**，不是当前重点。

## 目录结构

| 目录 | 内容 | 单一真相源规则 |
|---|---|---|
| `architecture/` | 系统核心设计：架构、骨架、通信、编排、插件契约与协议 | 每个主题一篇 |
| `decisions/` | ADR 架构决策记录（带日期，只追加不覆盖） | 每个决策一篇，永不过期 |
| `plugins/` | **具体插件**（平台之上开发的一个个插件）各自的文档 | 一插件一篇/目录 |
| `guides/` | 开发指南、部署指南、使用手册 | 按任务分篇 |

> 注意：插件**机制本身**的系统设计在 `architecture/`（《插件契约与协议》），`plugins/` 只放具体插件的文档。

## 文档维护纪律（保持"永远有效"的关键）

1. **单一真相源**——同一件事只有一篇权威文档。发现重复立即合并。
2. **代码与文档同一次改动**——改模块就在同一次提交里改它的设计文档，不事后补。
3. **决策进 ADR**——任何"为什么这样设计"的取舍写成 `decisions/` 下的 ADR，只追加不修改。
4. **过期即删**——宁可删掉过期文档，也不留误导性内容。

## 当前文档

### 架构（核心设计）
- [**系统架构**](architecture/系统架构.md) ⭐
  - 引言：定位与决策速览
  - 第一章 系统骨架（微内核 / 四内核 / 扩展点 / 生命周期）
  - 第二章 内核间与服务间通信（三通信轴 / 位置透明 / §2.8 寻址层接口 / §2.9 Wire 层）
  - 第三章 插件服务与部署编排（插件服务 / 节点代理 / 集群化 / §3.6 期望态 schema）
- [**插件契约与协议**](architecture/插件契约与协议.md) ⭐
  - 第一章 统一插件定义（清单 & 四面贡献点）
  - 第二章 插件-宿主协议（握手 / 调用 / 事件 / 生命周期）
  - 第三章 契约字段（CallContext / Target / Result / Event）
  - 第四章 扩展点契约（17 个扩展点的 descriptor + 分发语义）

### 决策记录（ADR）
- [ADR 使用说明](decisions/README.md)
- [ADR-0001 插件化架构模型：微内核 + 全层扩展点](decisions/ADR-0001-插件运行模型.md)
- [ADR-0002 技术栈全新选型](decisions/ADR-0002-技术栈选型.md)
- [ADR-0003 插件装载模型：运行时热装](decisions/ADR-0003-插件装载模型.md)
- [ADR-0004 插件运行形态：独立进程 + 协议总线](decisions/ADR-0004-插件运行形态.md)
- [ADR-0005 骨架设计与技术栈解耦](decisions/ADR-0005-骨架与技术栈解耦.md)
- [ADR-0006 内核分区与后端服务灵活组合（见 0014 扩为四内核）](decisions/ADR-0006-内核分区与后端组合.md)
- [ADR-0007 内核间与服务间通信模型](decisions/ADR-0007-内核间通信模型.md)
- [ADR-0008 骨架技术选型对比：go-plugin / Dapr / NATS](decisions/ADR-0008-骨架技术选型对比.md)
- [ADR-0009 内核技术栈选型（后端 Go / Runner Go / 前端 React / 移动 gomobile）](decisions/ADR-0009-内核技术栈选型.md)
- [ADR-0010 插件服务与部署编排](decisions/ADR-0010-插件服务与部署编排.md)
- [ADR-0011 组合是通用内核能力（服务/门户/客户端App）](decisions/ADR-0011-组合是通用内核能力.md)
- [ADR-0012 Runner（原 APP）内核运行模型：预编译 + 整体热升级](decisions/ADR-0012-APP内核运行模型.md)
- [ADR-0013 客户端多档能力：桌面完整 runner + 手机 Companion](decisions/ADR-0013-APP多档能力与手机Companion.md)
- [ADR-0014 四内核结构：拆出 Runner 内核与移动 Companion 内核](decisions/ADR-0014-四内核结构.md)
- [ADR-0015 内核与贡献面命名规范（backend/frontend/runner/mobile）](decisions/ADR-0015-内核与贡献面命名规范.md)
- [ADR-0016 单仓（monorepo）与代码目录布局](decisions/ADR-0016-单仓与代码目录布局.md)
- [ADR-0017 版本定义与兼容性机制](decisions/ADR-0017-版本定义与兼容性机制.md)
- [ADR-0018 测试布局与分层](decisions/ADR-0018-测试布局与分层.md)
- [ADR-0019 工程规范基线](decisions/ADR-0019-工程规范基线.md)
- [ADR-0020 代码设计原则与复用策略](decisions/ADR-0020-代码设计原则与复用策略.md)
- [ADR-0021 权限判定的强制点与零校验器语义](decisions/ADR-0021-权限判定强制点.md)
- [ADR-0022 Go 模块标识使用自有域名](decisions/ADR-0022-Go模块标识使用自有域名.md)
- [ADR-0023 插件 Schema 与可验证制品仓库](decisions/ADR-0023-插件Schema与可验证制品仓库.md)
- [ADR-0024 单节点自动装配与回滚语义](decisions/ADR-0024-单节点自动装配与回滚语义.md)
- [ADR-0025 NATS 控制面、能力寻址与多节点调度](decisions/ADR-0025-NATS控制面寻址与多节点调度.md)
- [ADR-0026 远端制品仓库与供应链信任](decisions/ADR-0026-远端制品仓库与供应链信任.md)
- [ADR-0027 NATS 生产安全与最小权限](decisions/ADR-0027-NATS生产安全与最小权限.md)
- [ADR-0028 控制器选主与 Drain 收敛](decisions/ADR-0028-控制器选主与Drain收敛.md)
- [ADR-0029 跨服务双向流与持久事件](decisions/ADR-0029-跨服务双向流与持久事件.md)
- [ADR-0030 资源感知、亲和与指标自动伸缩调度](decisions/ADR-0030-资源感知亲和与自动伸缩.md)
- [ADR-0031 Backend 内核 1.0 封板与工程门禁](decisions/ADR-0031-Backend内核1.0封板与工程门禁.md)

### 插件（具体插件文档）
- [说明](plugins/README.md) —— _具体插件开始开发后在此登记_

### 指南
- [代码目录结构](guides/代码目录结构.md) —— 代码放哪的单一真相源（活的目录参考）
- [测试规范](guides/测试规范.md) —— 测试放哪、怎么写、怎么跑
- [工程规范](guides/工程规范.md) —— 提交/分支/依赖许可证/编码规范/**代码设计原则**/单一真源铁律
- [远端制品仓库运行指南](guides/远端制品仓库.md) —— Ed25519 信任根、HTTPS 服务、发布与 Node Agent 接入
- [NATS 生产安全运行指南](guides/NATS生产安全.md) —— mTLS、NKey、角色 ACL 与安全接入
- [控制面高可用运行指南](guides/控制面高可用.md) —— Controller 多副本选主、接管与 Drain
- [高级调度运行指南](guides/高级调度.md) —— 节点容量、亲和/反亲和、外部指标自动伸缩
- [Backend 内核 1.0 封板指南](guides/Backend内核1.0封板.md) —— Backend 微内核封板范围、工程门禁与当前验收状态
- [ADR-0032 Backend 插件生命周期与实际态 v2](decisions/ADR-0032-Backend插件生命周期实际态v2.md) —— 当前实例/升级候选双视图、封闭状态机和 v1 状态迁移
- [ADR-0033 Backend 插件状态迁移事务](decisions/ADR-0033-Backend插件状态迁移事务.md) —— 显式状态格式、copy-on-write prepare/commit/rollback 与失败保旧
- [ADR-0034 Backend 协议资源边界](decisions/ADR-0034-Backend协议资源边界.md) —— Host/SDK/addressing 统一 payload、metadata、并发、队列、deadline 与 drain 硬边界
- [ADR-0035 Backend 可观测与健康契约](decisions/ADR-0035-Backend可观测与健康契约.md) —— slog、跨跳 trace、可替换 metric sink、健康就绪与无敏感诊断快照
- [ADR-0036 Backend 核心 SPI 边界](decisions/ADR-0036-Backend核心SPI边界.md) —— unit 配置、宿主凭证代理、scoped persistence/transaction 与插件会话身份注入
