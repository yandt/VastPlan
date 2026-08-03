# ADR-0190：Portal 插件自治导航策略与服务复用

- 状态：已接受；2026-08-02 由下述“插件自治导航修订”取代原语义映射实现
- 日期：2026-08-02

## 背景

功能插件原先把页面直接绑定到 `zone + groupID`。种子平台的功能因此全部落入 `settings`，而且同一插件进入另一个服务或 Portal 后仍携带种子平台的具体菜单位置。把页面 ID 集中重写到 Catalog 又会让 Portal 配置依赖每个插件的内部页面清单，破坏插件自发现和在线组合边界。

菜单属于 Portal 的信息架构，不属于 Backend 服务。一个 Portal 可以管理多个本地或远端服务；不同 Portal 也可以选择不同功能集合和布局。因此需要同时满足：同一 Portal 中各服务使用一致分类，不同 Portal 可以独立配置，插件本身仍可在没有策略时安全显示。

## 决策

导航采用三段式协议：

1. 功能插件的页面通过 `navigation.semanticID` 声明稳定功能语义，同时保留 `zone/groupID` 作为自包含默认位置。语义 ID 描述能力，不描述某个 Portal 的菜单文案和层级。
2. Frontend Platform Profile 的 `shell.config.navigationGroups` 继续拥有当前 Portal 的菜单树；新增 `navigationPlacements`，以精确的 `semanticID → groupID` 映射决定页面在该 Portal 中的实际位置。
3. Shell Composition 在组合根只编译一次策略。命中映射时，以目标分组的 zone 和层级为准；未命中时使用插件默认位置。布局插件只消费标准化结果，不读取策略，也不猜测领域分类。

同一 Portal 管理的所有服务共享一份导航策略。一个插件为多个 `serviceID` 注册页面时，相同 `semanticID` 自动进入相同菜单组。不同 Portal 的 WorkingCopy 各自保存完整 Platform Profile，可以从同一个 Seed 创建模板开始，但发布后独立审批、版本化和演进；本阶段不增加跨 Portal 的可变共享策略资源。

`navigationPlacements` 必须满足：语义 ID 唯一、目标分组存在、分组树仍只有 `root → child → page` 两级分组、映射只改变呈现位置。它不授予权限、不启用插件、不改变路由，也不能让未注册或无权限页面出现。Go Profile Schema、Portal Composer 和浏览器 Shell 使用同一字段协议；重复、未知或非法映射必须 fail-closed。

## 2026-08-02 插件自治导航修订

原方案仍把完整菜单树放在 Portal Profile，导致每次增删插件都要同步修改中央 Profile，不能满足 Catalog 自发现和实时 Generation 切换。现作以下破坏性修订；本节取代上文的 `semanticID`、页面 `zone/groupID`、`navigationGroups` 和 `navigationPlacements` 实现：

1. Manifest 只接受唯一强类型 `contributes.frontend.navigations`。每个插件最多声明一个 `main@1.0.0` 目录，可信宿主把节点身份固定为 `pluginId/nodeId`，页面只用 owner-bound `parentMenuRef` 引用节点。
2. 目录只允许一级、二级菜单组，Workbench 页面是第三级叶子。跨插件父级必须声明 `required` 或带本插件根节点回退的 `optional`；required 还必须有 Manifest 依赖。Contribution Index、Portal Runtime 和 Shell 共同拒绝重复、未知父级、循环、跨 zone 与超深树。
3. 菜单名称使用插件命名空间下的 `key + fallback`，解析顺序是当前语言、Portal 默认语言、Manifest fallback。切换语言只重渲染；目录、语言目录和模块必须属于同一个 Generation。排序始终按 `order + pluginId/nodeId`，不得按翻译文本排序。
4. Profile 只保留 `navigationOverrides[]`，可对已安装节点隐藏、排序、调整父级或覆盖受支持 locale 的名称；不得创建节点、启用插件、授予权限或修改路由。候选发布和浏览器切换都必须校验覆盖目标及最终树。
5. Shell 只保留 zone、账户、退出与恢复入口等安全锚。无可见页面的插件菜单自动隐藏；活动页面被移除时由统一导航模型选择同组、同级、首个可访问页，最后回到账户或恢复入口。
6. 自定义菜单图标在构建时归一化为受限 `path/group` AST，运行时不执行插件 SVG 或 React 代码。状态只允许 normal/active/loading/error，动效只允许 none/pulse/spin/draw，并服从 reduced-motion 与活动状态预算。

实现中原始 SVG 只能位于插件的 `frontend/icons/navigation/*.svg` 作者目录。Node 构建步骤在签名打包前完成 XML 白名单解析、`currentColor` 归一化、状态与体积预算校验并删除 source 路径；Go 打包器、仓库发布和供应链复验共同拒绝仍含 source 的制品。可信 Portal 物化阶段会把已验证 Contribution Index 与 `navigationOverrides` 合并校验，未知节点、循环、跨 zone 或超深覆盖在 Activation 候选形成前失败；浏览器仍重复校验以防传输或缓存损坏。

该修订与 UI Contract 10.0.0 同代生效，不保留旧页面导航字段的运行时双读；不可变历史制品只用于审计和旧 Generation 排空，不进入新候选。

## 种子平台默认策略

种子平台把日常治理能力放入五个主菜单：

| 主菜单 | 语义分类 |
|---|---|
| 服务与部署 | `platform.operations.deployment` |
| 制品与交付 | `platform.delivery.artifacts` |
| 资源与配置 | 全局设置、插件配置、凭证和数据库连接 |
| 集成与 API | `platform.integration.api-exposure` |
| 安全与授权 | `platform.security.authorization` |

`platform.portal.management` 保留在系统设置。原插件默认分组继续作为未配置策略时的安全回退，不作为种子平台的实际菜单位置。

## 结果

- 新服务启用已有插件时无需修改插件代码，当前 Portal 的策略自动决定菜单位置。
- 多服务单 Portal 保持一致；多个 Portal 可以按各自受众独立精简或调整菜单。
- 插件仍可脱离种子平台运行，缺少映射不会导致页面消失。
- 菜单策略不会成为权限旁路，也不会把具体页面 ID 固化进内核或 Catalog Resolver。
- 目录解析和语言回退在 512 节点硬上限内保持有界；测试以 500 节点覆盖解析与即时语言解析。活动页面随新 Generation 消失时，Shell 只对旧代确实拥有的页面执行同组、同根、首个可访问页的确定性替换，不吞掉未知深链路的 Not Found。

## 2026-08-03 账户锚点收敛修订

上述“Shell 只保留安全锚”的表述进一步收敛：Shell 不再内置 `primary`、`secondary`、`settings` 业务根节点，也不再内置 `account.settings` 子分组。它们曾使“系统管理”等业务分类看似来自插件、实际仍受 Shell 默认目录控制，且允许 Profile 把业务节点重新挂入账户区域。

现仅保留不可覆盖的 `vastplan.host/account` 账户头像根锚点。个人中心及其已声明扩展都直接以该锚点作为 `parentMenuRef`；账户二级分组如有需要，必须由个人中心实现未来以自身已签名的导航目录提供，不能由 Shell 预置。`primary`、`secondary`、`settings` 继续作为插件 Manifest 选择的布局区域，分别决定两种 Shell 的视觉承载位置，但不携带业务名称、图标、顺序或菜单树。

`navigationOverrides` 只能操作已安装的插件节点，并明确禁止修改账户锚点或将插件节点移入账户锚点。这样，账户入口保持认证与恢复所需的最小可信边界，所有其他一级、二级菜单仍保持插件安装即自发现、停用即消失的自治行为。
