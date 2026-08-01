# ADR-0183 页面正文语义尺寸与 Shell 统一居中

## 状态

已接受，2026-08-01。

## 背景

Portal Shell 过去只支持 Platform Profile 在模板级选择 `fluid` 或 `contained`（1280px）。所有页面共享同一上限，设置、向导和短表单只能在功能组件内部硬编码 `max-width`，无法由两种 Shell 统一居中，也无法让直接页面和 Workbench 页面复用同一规则。

ADR-0074 禁止功能插件设置页面宽度，目的是防止插件传入任意像素、绘制自己的 Page Shell 或破坏跨布局一致性。该限制不应阻止页面选择由平台预先治理的有限布局语义。

## 决策

1. UI Contract 8.4.0 增加 `PageBodyLayout = fluid | large | medium | small`。
2. `PortalPageDefinition` 以及 Collection、Workspace、Form、Record 四类 Workbench 页面都可以声明可选 `bodyLayout`；省略时等同 `fluid`。
3. 页面只能选择语义值，不能传入像素、CSS、组件或断点。SDK Helper 与 Portal Runtime 使用同一封闭值集合校验，绕过 Helper 的注册同样失败关闭。
4. 唯一宽度 Recipe 位于 `@vastplan/ui-primitives`：`large=1280px`、`medium=960px`、`small=720px`、`fluid` 不设置页面上限。
5. 标准侧栏与顶部导航 Shell 都读取活动页面的 `bodyLayout`，对整个 `page.body.before/main/aside/after` 内容区域设置最大宽度并通过 `margin-inline:auto` 居中。Page Header 继续占据工作区全宽且位于滚动容器之外。
6. Shell 模板级 `pageBodyWidth=contained` 保留为平台上限。页面尺寸只能进一步收窄；两者同时存在时取更小值。
7. 移动端继续使用可用宽度的 100%，`max-width` 不引入固定最小宽度或横向滚动。

## 结果

- 外观设置页面声明 `small`，不再自行保存像素宽度，并在两种 Shell 中一致居中。
- 普通集合页继续默认 `fluid`，不会因契约升级改变现有布局。
- ADR-0074 对“插件不得设置任意页面宽度”的限制继续有效；本 ADR 只增加受治理的语义选择，不授权插件控制 Shell 拓扑或样式。
