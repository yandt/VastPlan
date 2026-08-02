# ADR-0190：Portal 语义导航策略与服务复用

- 状态：已接受
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
