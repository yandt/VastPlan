# VastPlan 文档地图

> 本文件是全部项目文档的唯一索引入口。任何人（包括 AI 协作者）查阅文档都从这里出发。
> `CLAUDE.md` 会引用本文件，确保每次会话都能顺着它找到最新文档。

## 一句话定位

VastPlan 是一套**基于 LLM 的通用 Agent 系统**，面向企业级客户，支持在线 Agent 开发、远程连接任务客户端在本地运行脚本/工作流。系统采用**微内核 + 全层扩展点**架构：内核只提供最小骨架，绝大多数功能（审计、可观测、用户系统扩展、Studio 可开发模块等）都是骨架之上的**第一方插件**。内核分**四套**——Backend / Frontend / Runner（桌面客户端执行器）/ Mobile（手机 Companion），规范 ID `backend/frontend/runner/mobile`；后端内核可灵活组合出 backend/workspace/rs 等服务。

> 插件当前**全部由本方开发（第一方、可信）**，暂不开放第三方。清单、运行驱动 SPI 与发布者运行策略已预留第三方扩展；生产默认要求未知发布者至少 `process-sandbox`，内核使用者可用全局三态和发布者级优先规则决定信任边界（ADR-0048）。

## 目录结构

| 目录 | 内容 | 单一真相源规则 |
|---|---|---|
| `architecture/` | 系统核心设计：架构、骨架、通信、编排、插件契约与协议 | 每个主题一篇 |
| `decisions/` | ADR 架构决策记录（带日期，只追加不覆盖） | 每个决策一篇，永不过期 |
| `extensions/plugins/` | **具体插件**（平台之上开发的一个个插件）各自的文档 | 一插件一篇/目录 |
| `guides/` | 开发指南、部署指南、使用手册 | 按任务分篇 |

> 注意：插件**机制本身**的系统设计在 `architecture/`（《插件契约与协议》），`extensions/plugins/` 只放具体插件的文档。

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
  - 第三章 制品服务与部署编排（内核信任/种子基座 / 仓库基础插件 / 控制面 / 节点代理）
- [**插件契约与协议**](architecture/插件契约与协议.md) ⭐
  - 第一章 统一插件定义（清单 & 四面贡献点）
  - 第二章 插件-宿主协议（握手 / 调用 / 事件 / 生命周期）
  - 第三章 契约字段（CallContext / Target / Result / Event）
  - 第四章 扩展点契约（18 个扩展点的 descriptor + 分发语义）
- [**插件服务集群化设计**](architecture/插件服务集群化设计.md)
  - 插件实例策略、能力可见性、路由域、A/B/C 平台服务集群与故障恢复
- [**插件分级与组合解析**](architecture/插件分级与组合解析.md)
  - foundation/platform/application 管理边界、Platform Profile、Application Composition 与解析锁
- [**前端门户内核**](architecture/前端门户内核.md)
  - Portal 启动壳、设计系统插件、多 UI 框架、动态表单与在线组合发布治理
- [**跨端体验与交互契约**](architecture/跨端体验与交互契约.md)
  - Portal、Mobile、Runner 的声明式 UI 语义、交互 Broker 与安全边界

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
- [ADR-0032 Backend 插件生命周期与实际态 v2](decisions/ADR-0032-Backend插件生命周期实际态v2.md)
- [ADR-0033 Backend 插件状态迁移事务](decisions/ADR-0033-Backend插件状态迁移事务.md)
- [ADR-0034 Backend 协议资源边界](decisions/ADR-0034-Backend协议资源边界.md)
- [ADR-0035 Backend 可观测与健康契约](decisions/ADR-0035-Backend可观测与健康契约.md)
- [ADR-0036 Backend 核心 SPI 边界](decisions/ADR-0036-Backend核心SPI边界.md)
- [ADR-0037 Backend 可靠性与性能门禁](decisions/ADR-0037-Backend可靠性与性能门禁.md)
- [ADR-0038 Backend 可复现发布与运维交付](decisions/ADR-0038-Backend可复现发布与运维交付.md)
- [ADR-0039 Backend 能力调用环保护](decisions/ADR-0039-Backend能力调用环保护.md)
- [ADR-0040 Backend 生产入口与包边界](decisions/ADR-0040-Backend生产入口与包边界.md)
- [ADR-0041 Go 契约类型与 CAS 模板单一真源](decisions/ADR-0041-Go契约类型与CAS模板单一真源.md)
- [ADR-0042 复杂度分层与 CI 质量门禁](decisions/ADR-0042-复杂度分层与CI质量门禁.md)
- [ADR-0043 插件启动授权与签名时间边界](decisions/ADR-0043-插件启动授权与签名时间边界.md)
- [ADR-0044 全局依赖编排与本地自治启动管理](decisions/ADR-0044-全局依赖编排与本地自治启动管理.md)
- [ADR-0045 插件实例化策略与服务集群化边界](decisions/ADR-0045-插件实例化策略与服务集群化边界.md)
- [ADR-0046 Apache-2.0 开源许可与插件制品声明](decisions/ADR-0046-Apache开源许可与插件制品声明.md)
- [ADR-0047 多语言运行驱动与第三方隔离边界](decisions/ADR-0047-多语言运行驱动与第三方隔离边界.md)
- [ADR-0048 发布者级插件运行策略](decisions/ADR-0048-发布者级插件运行策略.md)
- [ADR-0049 制品信任基座与仓库基础插件](decisions/ADR-0049-制品信任基座与仓库基础插件.md)
- [ADR-0050 首方插件多级命名空间与自举权限基线](decisions/ADR-0050-首方插件多级命名空间与自举权限基线.md)
- [ADR-0051 Backend 混合插件运行与受控内嵌边界](decisions/ADR-0051-Backend混合插件运行与受控内嵌边界.md)
- [ADR-0052 前端门户内核与多 UI 设计系统插件](decisions/ADR-0052-前端门户内核与多UI设计系统插件.md)
- [ADR-0053 门户访问策略作为独立基础插件](decisions/ADR-0053-门户访问策略插件.md)
- [ADR-0054 跨端体验契约与交互协调](decisions/ADR-0054-跨端体验契约与交互协调.md)
- [ADR-0055 交互访问策略作为独立基础插件](decisions/ADR-0055-交互访问策略作为独立基础插件.md)
- [ADR-0056 App Profile 独立契约与部署引用](decisions/ADR-0056-App-Profile独立契约与部署引用.md)
- [ADR-0057 插件分级管理与双输入组合解析](decisions/ADR-0057-插件分级管理与双输入组合解析.md)
- [ADR-0058 跨内核组合公共契约与内核适配器](decisions/ADR-0058-跨内核组合公共契约与适配器.md)
- [ADR-0059 Frontend 双输入采用服务端权威解析](decisions/ADR-0059-Frontend双输入服务端权威解析.md)
- [ADR-0060 五大区域仓库布局与根目录收敛](decisions/ADR-0060-五大区域仓库布局与根目录收敛.md)

### 插件（具体插件文档）
- [说明](plugins/README.md) —— 具体插件文档规则
- [Python Hello 参考插件](plugins/com.vastplan.python-hello.md) —— Python SDK、事件发布与跨语言调用参考
- [自举权限基线](plugins/com.vastplan.foundation.security.bootstrap-policy.md) —— 首方多级命名空间、settings 写保护与最低权限基线
- [全局设置基础插件](plugins/com.vastplan.platform.configuration.global-settings.md) —— 租户隔离设置、版本前置条件、变更游标与 leader 状态边界
- [凭证管理基础插件](plugins/com.vastplan.platform.security.credentials.md) —— Vault Transit 信封加密、元数据 API 与不返回明文的安全边界
- [数据库连接基础插件](plugins/com.vastplan.platform.data.relational.connection-manager.md) —— 连接定义、CredentialRef 与可信宿主连通性检查
- [制品仓库基础插件](plugins/com.vastplan.platform.artifacts.repository.md) —— HTTPS 发布/读取、内核信任适配与兼容自举边界

### 指南
- [当前系统开发成果（非技术版）](guides/当前系统开发成果.md) —— 面向管理层、产品和业务人员的阶段成果、完成边界与下一步方案
- [代码目录结构](guides/代码目录结构.md) —— 代码放哪的单一真相源（活的目录参考）
- [Graphify 主图维护](guides/Graphify图谱维护.md) —— 主图收录边界、降噪、更新命令与查询纪律
- [多语言插件开发](guides/多语言插件开发.md) —— Go/Python SDK、执行契约、能力协商与第三方隔离边界
- [测试规范](guides/测试规范.md) —— 测试放哪、怎么写、怎么跑
- [工程规范](guides/工程规范.md) —— 提交/分支/依赖许可证/编码规范/**代码设计原则**/单一真源铁律
- [远端制品仓库运行指南](guides/远端制品仓库.md) —— Ed25519 信任根、HTTPS 服务、发布与 Node Agent 接入
- [NATS 生产安全运行指南](guides/NATS生产安全.md) —— mTLS、NKey、角色 ACL 与安全接入
- [控制面高可用运行指南](guides/控制面高可用.md) —— Controller 多副本选主、接管与 Drain
- [高级调度运行指南](guides/高级调度.md) —— 节点容量、亲和/反亲和、外部指标自动伸缩
- [Backend 内核 1.0 封板指南](guides/Backend内核1.0封板.md) —— Backend 微内核封板范围、工程门禁与当前验收状态
- [Backend 发布与运维](guides/Backend发布与运维.md) —— 可复现发布、制品证明、配置迁移、升级回滚与脱敏支持包
