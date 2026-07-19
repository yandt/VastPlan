# ADR-0083 前端 UI 分层术语与插件命名空间

- 状态：已采纳
- 日期：2026-07-19
- 关联：[ADR-0050](ADR-0050-首方插件多级命名空间与自举权限基线.md)、[ADR-0052](ADR-0052-前端门户内核与多UI设计系统插件.md)、[ADR-0082](ADR-0082-前端工作台组合框架.md)

## 背景

此前前端基础层同时使用 `shell-*`、`design-system` 和 `workbench` 三种命名视角：前两者的一部分描述页面区域，一部分描述设计理念，后者描述产品能力。阅读扩展点或插件 ID 时，无法直接判断它处理的是页面结构、框架渲染，还是业务页面工作流。

首方插件也长期使用 `com.vastplan.*`。项目的 Go 模块和 Schema 已使用 `cdsoft.com.cn` 自有域名，首方插件命名空间应与组织域和中国部署语境保持一致。

## 决策

1. 前端 UI 使用统一三层术语，扩展点固定遵循 `ui.<layer>.<role>`：

   | 层 | 扩展点 | 唯一职责 |
   |---|---|---|
   | 结构 | `ui.structure.composition` | 定义 Slot、页面/导航拓扑、内容归并和顺序 |
   | 结构 | `ui.structure.layout` | 将已确定的结构排布为侧栏、顶栏和页面区域；不改变业务行为 |
   | 渲染 | `ui.render.adapter` | 将框架无关的 UI primitives 映射到 Arco、MUI 或未来框架，拥有主题、DOM、焦点和框架私有渲染 |
   | 工作流 | `ui.workflow.workbench` | 组合列表、卡片、表单、动作与 Overlay 的通用页面状态机；仍为 ADR-0082 的待实施目标 |

2. “设计系统”保留为面向人类的视觉规范概念，不再作为扩展点职责名。具体插件通过 `ui.render.adapter` 说明其内核角色是渲染适配，而非页面结构或业务工作流。
3. UI Contract 升级为 3.0。Platform Profile、Portal Runtime、Catalog、插件 Manifest 和公共 TypeScript 类型使用 `renderAdapter`、`structureComposition`、`structureLayout` 三个明确字段；开发阶段不保留 2.x 双运行时或旧字段别名。
4. 内部基础组件 SDK 从 `@vastplan/portal-ui` 更名为 `@vastplan/ui-primitives`。它只允许渲染适配、结构基础插件和未来 Workbench 使用；功能插件仍只能面向 `@vastplan/workbench-sdk`。
5. 所有首方插件 ID、目录、配置引用、测试夹具和文档从 `com.vastplan.*` 一次性迁移为 `cn.vastplan.*`。`cn.vastplan.*` 与 `publisher=vastplan` 的双向绑定继续由内核强制校验，不保留旧前缀兼容。

## 后果

- 正面：从名称即可判断一个插件能否改变结构、渲染或业务工作流，减少 Profile 配置和代码审查中的边界误判。
- 正面：Arco/MUI 是可替换的 render adapter；布局替换不会影响 Workbench 行为；Workbench 不能反向修改 Slot 拓扑，三类扩展演进互不混淆。
- 代价：这是破坏性迁移。已有 Portal Profile、制品、自动化脚本和外部 SDK 引用必须整体切换到 3.0 与 `cn.vastplan.*` 后才能装配。
- 不采用别名：双字段、双扩展点或双命名空间会让运行时、Catalog 和文档永久保留模糊映射；项目尚处开发阶段，应一次性消除。

## 验收

1. Manifest Schema 仅接受 `renderAdapters`、`structureCompositions`、`structureLayouts` 贡献字段；Portal Runtime 仅接受三种对应扩展点。
2. Platform Profile 的 JSON 仅接受 `renderAdapter`、`structureComposition`、`structureLayout`，并要求三者为不同的首方插件。
3. `pluginid` 仅将 `cn.vastplan.*` 判定为首方命名空间；`com.vastplan.*` 被拒绝为 `publisher=vastplan` 的插件 ID。
4. TypeScript 全工作区类型检查、Frontend Portal Runtime 测试、Schema/Catalog/Composer 测试通过。
