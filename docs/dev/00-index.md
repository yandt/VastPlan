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
| `design/` | Portal 跨布局、跨 UI 框架的视觉、交互与无障碍规范 | 一份设计系统基线 |
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

### 后续任务
- [**后续任务清单**](../../TODOS.md) —— 只记录已明确推迟且具备上下文、依赖与安全边界的跨阶段工作

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
- [**插件配置与托管凭证**](architecture/插件配置与托管凭证.md)
  - 插件私有配置信封、内联秘密输入、CredentialRef 句柄、两阶段生效与 Provider 选择
- [**前端门户内核**](architecture/前端门户内核.md)
  - Portal 启动壳、设计系统插件、多 UI 框架、动态表单与在线组合发布治理
- [**Portal Shell Catalog 与按需 Library**](architecture/Portal-Shell-Catalog与按需Library.md)
  - 稳定 Slot/Composition、按需 Shell Library、PageOutlet 与用户级候选切换
- [**统一 Render Adapter 与可切换 Renderer**](decisions/ADR-0087-统一Render-Adapter与可切换Renderer.md)
  - Arco/MUI 内部 Renderer、Profile 治理与安全整代切换边界
- [**UI 工作台组合框架**](architecture/UI工作台组合框架.md)
  - 跨插件的列表、卡片、动作、表单与 Overlay 工作流组合规范（目标设计）
- [**平台管理中心**](architecture/平台管理中心.md)
  - 领域插件自有页面、强类型 BFF、远端平台能力寻址与细粒度权限
- [**在线角色与权限治理**](architecture/在线角色与权限治理.md)
  - 插件权限目录、Authorization IR/Policy Domain、四类 Provider Protocol、在线角色 revision、签名策略快照与每内核就近强制
- [**登录与认证协议**](architecture/登录与认证协议.md)
  - 会话前 Access Profile、统一登录工作台、密码/验证码 Method Provider、短时 Assertion 与 Node Session 签发
- [**企业身份与种子访问**](architecture/企业身份与种子访问.md)
  - 无内置普通用户系统、可选择的认证 Provider、数据库无关 Seed Access、交接与灾难恢复
- [**API 暴露与数据面服务**](architecture/API暴露与数据面服务.md)
  - 治理式 API Contract/Exposure、稳定随机 Route Key、Node Gateway 与独立数据面 Endpoint Lease
- [**服务部署控制台**](architecture/服务部署控制台.md)
  - Linux SSH 首次引导、systemd Node Agent 接管、在线服务组合与副本配置边界
- [**跨端体验与交互契约**](architecture/跨端体验与交互契约.md)
  - Portal、Mobile、Runner 的声明式 UI 语义、交互 Broker 与安全边界

### 设计系统
- [**Portal 设计系统**](design/DESIGN.md)
  - Shell/Overlay 语义 token、两种布局、三级导航、管理工作区、响应式与无障碍基线

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
- [ADR-0061 统一调用信封与受众投影](decisions/ADR-0061-统一调用信封与受众投影.md)
- [ADR-0062 Frontend 可信 ESM 制品与运行描述](decisions/ADR-0062-Frontend可信ESM制品与运行描述.md)
- [ADR-0063 Portal 静态宿主与样式隔离](decisions/ADR-0063-Portal静态宿主与样式隔离.md)
- [ADR-0064 Portal 语义组件契约与动态表单运行时](decisions/ADR-0064-Portal语义组件契约与动态表单运行时.md)
- [ADR-0065 通用 JSON Schema 表单与 Arco 主题适配](decisions/ADR-0065-通用JSON-Schema表单与Arco主题适配.md)
- [ADR-0066 Arco 按需构建与单文件制品边界](decisions/ADR-0066-Arco按需构建与单文件制品边界.md)
- [ADR-0067 Portal 控制面闭环、安全恢复与第二适配器验收](decisions/ADR-0067-Portal控制面闭环安全恢复与第二适配器验收.md)
- [ADR-0068 分布式平台管理中心与强类型 BFF](decisions/ADR-0068-分布式平台管理中心与强类型BFF.md)
- [ADR-0069 Linux SSH 首次引导与 Node Agent 接管](decisions/ADR-0069-SSH首次引导与Node-Agent接管.md)
- [ADR-0070 Deployment Manager 与可信引导执行边界](decisions/ADR-0070-Deployment-Manager与可信引导执行边界.md)
- [ADR-0071 签名 Node Lease 与可信就绪判定](decisions/ADR-0071-签名Node-Lease与可信就绪判定.md)
- [ADR-0072 CSP 安全的浏览器 JSON Schema 校验](decisions/ADR-0072-CSP安全JSON-Schema校验.md)
- [ADR-0073 Portal 内容寻址交付快照](decisions/ADR-0073-Portal内容寻址交付快照.md)
- [ADR-0074 Portal 组合 Slot 与纯布局插件分层](decisions/ADR-0074-Portal组合Slot与纯布局插件分层.md)
- [ADR-0075 Portal 管理绑定与多平台基线](decisions/ADR-0075-Portal管理绑定与多平台基线.md)
- [ADR-0076 Portal Edge 分布式快照交付](decisions/ADR-0076-Portal-Edge分布式快照交付.md)
- [ADR-0077 Backend 在线组合与可信发布边界](decisions/ADR-0077-Backend在线组合与可信发布边界.md)
- [ADR-0078 Frontend 事务式热替换与插件生命周期](decisions/ADR-0078-Frontend事务式热替换与插件生命周期.md)
- [ADR-0079 Frontend 框架无关多语种契约](decisions/ADR-0079-Frontend框架无关多语种契约.md)
- [ADR-0080 Portal 三级导航与可切换布局](decisions/ADR-0080-Portal三级导航与可切换布局.md)
- [ADR-0081 Portal 治理与不可变 Activation](decisions/ADR-0081-Portal治理与不可变Activation.md)
- [ADR-0082 前端工作台组合框架](decisions/ADR-0082-前端工作台组合框架.md)
- [ADR-0083 前端 UI 分层术语与插件命名空间](decisions/ADR-0083-前端UI分层术语与插件命名空间.md)
- [ADR-0084 主题与工作台展示档位](decisions/ADR-0084-主题与工作台展示档位.md)
- [ADR-0085 渲染适配器主题模板契约](decisions/ADR-0085-渲染适配器主题模板契约.md)
- [ADR-0086 单 Shell 插件与可切换布局模板](decisions/ADR-0086-单Shell插件与可切换布局模板.md)
- [ADR-0087 统一 Render Adapter 与可切换 Renderer](decisions/ADR-0087-统一Render-Adapter与可切换Renderer.md)
- [ADR-0088 Backend 统一执行驱动与托管语言运行时](decisions/ADR-0088-Backend统一执行驱动与托管语言运行时.md)
- [ADR-0089 Runtime Provider 与共享 Host 池](decisions/ADR-0089-Runtime-Provider与共享Host池.md)
- [ADR-0090 插件配置与托管凭证闭环](decisions/ADR-0090-插件配置与托管凭证闭环.md)
- [ADR-0091 制品存储 Provider 供给边界](decisions/ADR-0091-制品存储Provider供给边界.md)
- [ADR-0092 业务插件拥有托管凭证生命周期](decisions/ADR-0092-业务插件拥有托管凭证生命周期.md)
- [ADR-0093 可信宿主加密 Material Lease](decisions/ADR-0093-可信宿主加密Material-Lease.md)
- [ADR-0094 操作系统 Guardian 与独立进程故障收敛](decisions/ADR-0094-操作系统Guardian与独立进程故障收敛.md)
- [ADR-0095 Database Runtime、多 Provider 连接池与集群事务](decisions/ADR-0095-Database-Runtime多Provider连接池与集群事务.md)
- [ADR-0096 配置入口 Codec 与 YAML 嵌套](decisions/ADR-0096-配置入口Codec与YAML嵌套.md)
- [ADR-0097 测试制品仓库与前端分级热升级](decisions/ADR-0097-测试制品仓库与前端分级热升级.md)
- [ADR-0098 制品依赖解析、精确锁与离线 Bundle](decisions/ADR-0098-制品依赖解析锁与离线Bundle.md)
- [ADR-0099 File Volume 在线迁移与可回滚切换](decisions/ADR-0099-File-Volume在线迁移与可回滚切换.md)
- [ADR-0100 制品生命周期、引用保护与垃圾回收](decisions/ADR-0100-制品生命周期引用保护与垃圾回收.md)
- [ADR-0101 离线 Bootstrap Inventory 与 LKG 推进](decisions/ADR-0101-离线Bootstrap-Inventory与LKG推进.md)
- [ADR-0102 可信宿主仓库自升级事务](decisions/ADR-0102-可信宿主仓库自升级事务.md)
- [ADR-0103 Node Portal Kernel 渐进替代 Go Portal Edge](decisions/ADR-0103-Node-Portal-Kernel渐进替代Go-Edge.md)
- [ADR-0104 Frontend Runtime Engine 与 React 单实现](decisions/ADR-0104-Frontend-Runtime-Engine与React单实现.md)
- [ADR-0105 可信多文件前端模块图与双端 Generation](decisions/ADR-0105-可信多文件前端模块图与双端Generation.md)
- [ADR-0106 多端统一身份授权与 Runner 执行租约](decisions/ADR-0106-多端统一身份授权与Runner执行租约.md)
- [ADR-0107 插件权限目录与系统管理授权治理](decisions/ADR-0107-插件权限目录与系统管理授权治理.md)
- [ADR-0108 会话前 Access Profile 与认证方法协议](decisions/ADR-0108-会话前Access-Profile与认证方法协议.md)
- [ADR-0109 种子访问与企业身份 Provider 分离](decisions/ADR-0109-种子访问与企业身份Provider分离.md)
- [ADR-0110 治理式 API Exposure 与独立数据面](decisions/ADR-0110-治理式API-Exposure与独立数据面.md)

### 制品仓库与测试发布

- [制品仓库与测试发布](architecture/制品仓库与测试发布.md) —— Seed/托管仓库边界、目录与包解析、测试发布、Provider 迁移和生命周期治理

### 插件（具体插件文档）
- [说明](plugins/README.md) —— 具体插件文档规则
- [Python Hello 参考插件](plugins/cn.vastplan.python-hello.md) —— Python SDK、事件发布与跨语言调用参考
- [自举权限基线](plugins/cn.vastplan.foundation.security.bootstrap-policy.md) —— 首方多级命名空间、settings 写保护与最低权限基线
- [平台管理访问策略](plugins/cn.vastplan.foundation.security.platform-admin-access-policy.md) —— 五个管理领域的角色授权与受限宿主回调
- [Database Runtime 基础插件](plugins/cn.vastplan.foundation.data.relational.runtime.md) —— 关系数据库 wire 契约、Provider SPI、错误分类与可信数据面边界
- [节点部署管理服务](plugins/cn.vastplan.platform.infrastructure.deployment-manager.md) —— 节点引导、在线服务组合、异人审批与可信发布
- [Portal Composer](plugins/cn.vastplan.platform.configuration.portal-composer.md) —— Portal 分域治理、不可变 Activation、Frontend Test Release 与引用保护
- [全局设置基础插件](plugins/cn.vastplan.platform.configuration.global-settings.md) —— 租户隔离设置、版本前置条件、变更游标与 leader 状态边界
- [凭证管理基础插件](plugins/cn.vastplan.platform.security.credentials.md) —— Vault Transit 信封加密、元数据 API 与不返回明文的安全边界
- [数据库连接基础插件](plugins/cn.vastplan.platform.data.relational.connection-manager.md) —— 连接管理面、CredentialRef 与 Database Runtime 规划边界
- [制品仓库基础插件](plugins/cn.vastplan.platform.artifacts.repository.md) —— HTTPS 发布/读取、Catalog、Publish Journal、内核信任适配与兼容自举边界
- [API Exposure 治理插件](plugins/cn.vastplan.platform.integration.api-exposure.md) —— 受治理 API Contract、随机 Route Key、Gateway Catalog、Endpoint Lease 与一次性 Ticket
- [本地文件制品存储 Provider](plugins/cn.vastplan.platform.artifacts.storage.file.md) —— 私有 volume 供给、路径隔离与非 RPC 数据面

### 指南
- [当前系统开发成果（非技术版）](guides/当前系统开发成果.md) —— 面向管理层、产品和业务人员的阶段成果、完成边界与下一步方案
- [代码目录结构](guides/代码目录结构.md) —— 代码放哪的单一真相源（活的目录参考）
- [多语言插件开发](guides/多语言插件开发.md) —— Go/Python SDK、执行契约、能力协商与第三方隔离边界
- [测试规范](guides/测试规范.md) —— 测试放哪、怎么写、怎么跑
- [工程规范](guides/工程规范.md) —— 提交/分支/依赖许可证/编码规范/**代码设计原则**/单一真源铁律
- [远端制品仓库运行指南](guides/远端制品仓库.md) —— Ed25519 信任根、HTTPS 服务、发布与 Node Agent 接入
- [NATS 生产安全运行指南](guides/NATS生产安全.md) —— mTLS、NKey、角色 ACL 与安全接入
- [控制面高可用运行指南](guides/控制面高可用.md) —— Controller 多副本选主、接管与 Drain
- [高级调度运行指南](guides/高级调度.md) —— 节点容量、亲和/反亲和、外部指标自动伸缩
- [Backend 内核 1.0 封板指南](guides/Backend内核1.0封板.md) —— Backend 微内核封板范围、工程门禁与当前验收状态
- [Backend 发布与运维](guides/Backend发布与运维.md) —— 可复现发布、制品证明、配置迁移、升级回滚与脱敏支持包
- [Linux 节点 SSH 首次引导](guides/Linux节点SSH引导.md) —— strict known_hosts、内核摘要校验、systemd Node Agent 接管
- [YAML 启动配置](guides/YAML启动配置.md) —— YAML/JSON 入口、嵌套 include 与规范 JSON 安全边界
- [本地平台管理中心](guides/本地平台管理中心.md) —— 一键构建并启动本机平台服务、Node Portal Kernel 与管理页面
