# ADR-0179 Ant Design 单实现与 Renderer 协议保留

- 状态：已采纳
- 日期：2026-07-31

## 背景

Portal 已实现统一 Render Adapter、可信 Renderer Catalog、按需模块图和 Host Epoch 切换，也曾同时维护 Ant Design、Arco 与 MUI 三套完整实现。三套实现需要持续同步每个语义组件、动态表单、主题、图标、无障碍、测试和构建预算，在产品功能仍快速建设时显著放大开发成本。

## 决策

1. 当前产品只交付并维护 `cn.vastplan.foundation.frontend.render.adapter.antd`。
2. 删除 Arco 与 MUI Renderer 插件、第三方依赖、专用构建门禁、Catalog 引用和登录外观投影，不保留弃用源码或空壳插件。
3. 保留通用 `ui.render.adapter`、Renderer Catalog、Platform Profile 字段、可信模块图、按需加载、契约校验和 Host Epoch 切换机制。内核与功能插件不得硬编码 Ant Design。
4. 默认 Platform Profile 只允许 `antd` 且不显示无意义的 Renderer 切换；主题、明暗模式和图标主题仍可由用户在 Ant Design 范围内选择。
5. 通用协议测试使用抽象主/备 Renderer fixture 验证多插件能力，不重新引入第二套生产 UI 依赖。
6. 未来新增 Renderer 必须以独立签名插件提供完整 UI Contract，实现 Workbench、表单、主题、图标、i18n、Shadow DOM 和无障碍一致性门禁后，才可加入 Catalog。

## 影响

- 当前前端依赖、构建时间、制品体积和组件同步工作显著下降。
- 功能插件继续只使用 Workbench 与框架无关 UI 契约，未来增加 Renderer 不需要修改业务页面。
- ADR-0065、ADR-0066、ADR-0067 与 ADR-0087 中关于已交付 Arco/MUI 实现的内容保留为历史记录；当前产品事实以本 ADR 和《前端门户内核》为准。

## 后续工程收口

本次删除同时暴露出开发发布输入仍受工作区总 `pnpm-lock.yaml` 影响：即使某个前端基础插件未直接依赖被删除框架，也可能因依赖证据重新生成而产生新的 stable 制品摘要。后续应把前端制品身份收敛为“插件源码 + 实际模块图 + 该插件可达依赖闭包 + 工具链身份”，生成插件级锁定证据；不得把未被插件使用的工作区依赖变化传播成该插件的新身份。

Seed 也应继续区分“可离线恢复的最小引导/LKG”与“仓库按 Profile 在线安装的 Foundation、Platform、Application 制品”。当前开发 Seed 仍预装精确基础闭包以保证离线启动，因此删除基础 Renderer 需要更新 Seed；这不等同于最终的全在线安装模型。
