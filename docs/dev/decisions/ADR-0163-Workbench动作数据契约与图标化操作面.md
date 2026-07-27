# ADR-0163 Workbench 动作数据契约与图标化操作面

- 状态：已采纳并实施
- 日期：2026-07-28
- 关联：[ADR-0082](ADR-0082-前端工作台组合框架.md)、[ADR-0087](ADR-0087-统一Render-Adapter与可切换Renderer.md)、[ADR-0159](ADR-0159-Ant-Design首选Renderer与按需交付.md)

## 背景

Workbench 已经用 `ActionSpec` 表达页面、集合、行、卡片和记录动作，但 `icon` 仍为可选字段，部分功能插件依赖文字按钮或运行时默认图标。这样会造成同一动作在不同页面和 Renderer 中呈现不一致，也让 Workbench 无法保证紧凑操作区、Tooltip 和无障碍名称。

另一个风险是把逐按钮回调继续向组合组件传递。回调与 React 节点一旦进入页面定义，插件就会重新取得 UI 结构控制权，页面定义也不再能被检查、投影和跨 Renderer 复用。

## 决策

1. UI Contract 升级为 `5.0.0`。所有 `ActionSpec` 必须显式声明稳定 `id`、本地化 `label`、语义 `icon` 和 `placement`；图标不再允许省略。
2. 图标只能来自 UI Contract 的 `SemanticIconName` 词表。现有词表无法准确表达新动作时，先扩充统一语义词表，并同时补齐 canonical、Ant Design、Arco 和 MUI 映射；禁止使用动作 ID 猜图标，也禁止功能插件传入 SVG、框架图标或 React 节点。
3. 页面定义不接受逐动作 `onClick`。表单和 Overlay 动作只引用受治理的 `form` / `overlay` ID；其他动作统一进入页面级 `runAction(context, signal)` 工作流端口，由 `action.id`、选中记录和取消信号驱动。
4. `defineCollectionPage` 与三类 Record 定义入口校验动作 ID 唯一、语义图标存在、引用目标有效，以及命令型动作是否存在 `runAction`。Portal Runtime 对插件绕过 SDK Helper 的情况重复执行同一验证并以 `WORKBENCH_PAGE_REJECTED` 失败关闭。
5. 表格末列“操作”保持居中。直接动作只显示语义图标，完整本地化标题由统一 `IconButton` Tooltip 和无障碍名称提供；最多两个动作直接展示，剩余动作进入仍由图标与 Tooltip 组成的“更多行操作”浮层。
6. 页面 Header、集合工具动作、卡片 Footer 和记录详情动作同样消费 `ActionSpec.icon`，不得回退到根据位置或 ID 推断的默认业务图标。批量动作的 Select 保留文字标签以避免误操作，执行按钮使用当前选中动作的语义图标。

## 备选方案

- 缺失时统一显示“更多”图标：兼容成本低，但不能区分编辑、删除、审批等高风险操作，拒绝。
- 按动作 ID 建立全局猜测表：看似数据驱动，实际把业务命名规则耦合进 Workbench，且第三方插件容易冲突，拒绝。
- 每个按钮直接接收回调和框架图标：最灵活，但破坏组合契约、跨 Renderer 一致性和可信宿主校验，拒绝。

## 影响

- 旧的 UI Contract 4.x 页面定义不能冒充兼容 5.x；开发阶段一次性迁移，不保留可选图标兼容分支。
- 新增动作时，插件作者必须先选择准确的语义图标；确无合适图标时扩充统一图标库，而不是使用模糊回退。
- Workbench 可以在不读取业务代码的情况下统一布局、Tooltip、危险色、权限投影和动作审计入口。
