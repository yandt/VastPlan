# ADR-0165 Contract Registry 与插件发布编排

- 状态：已采纳并完成首版
- 日期：2026-07-28
- 关联：[ADR-0017](ADR-0017-版本定义与兼容性机制.md)、[ADR-0097](ADR-0097-测试制品仓库与前端分级热升级.md)、[ADR-0098](ADR-0098-制品依赖解析锁与离线Bundle.md)、[ADR-0144](ADR-0144-组合服务代际热升级与插件动态调试.md)、[ADR-0145](ADR-0145-本地测试与正式远端制品仓库双协议.md)

## 背景

公共契约升级和插件 SemVer 变更会同时影响 SDK 常量、Schema 约束、插件兼容声明、Foundation Catalog、Backend/Portal Profile、制品构建、测试仓库回执与运行 Generation。此前各强制点能够 fail-closed，却主要在链路后段报告不同步；开发者仍需人工找到所有复制点，遗漏后才在构建或 Portal 安全启动阶段发现。

单纯批量替换版本会自动扩大插件兼容声明，无法证明插件已适配破坏性契约；把所有插件版本集中到一个超级配置，又会破坏 Manifest 作为签名制品身份真源的边界。

## 决策

1. `contracts/registry.yaml` 是公共跨模块契约版本的唯一结构化真源。首个登记项为 `frontend.ui`，以后可按同一模型增加其他 SemVer 契约。
2. Registry 生成 TypeScript/Go 常量和独立 JSON Schema 资源。手写 SDK、Foundation 模块和测试夹具应消费生成常量，不复制版本字面量。
3. 插件 `vastplan.plugin.json#version` 继续是插件版本唯一真源；插件自己的 `uiContract`/依赖范围属于发布者兼容承诺，生成器只验证，不自动修改。公共契约 major 变化时，未显式声明兼容的插件阻断发布。
4. Foundation Render Adapter 与 Shell 源码中的子插件精确版本改为 Manifest 派生的生成文件；Backend/Portal 部署 JSON 中的精确插件引用由发布编排器按 Manifest 更新，运行代码不再维护第二套版本表。
5. `pluginrelease` 读取一份 Release Spec，构造全工作区依赖图、检查循环和版本范围、计算反向影响、按依赖顺序生成确定性 Release Plan，并同步机械派生文件。
6. 开发 `execute` 复用现有 `pluginpackage -> artifact.repository.local-test.v1/workspace -> Test Release -> Service/Portal Generation` 链路。它不直连 Runtime，不让仓库取得激活权限，也不在内核启动时执行。
7. 生产 `execute` 只准备并输出待审批计划，不构建、上传或激活制品。正式发布继续使用远端仓库、供应链证据、异人审批和发布控制器。
8. Release Spec 不接受任意 shell 命令；构建器按 Manifest 自动发现 Backend/Frontend 入口及语言驱动。Runner/Mobile 尚未接入统一 Test Release 时明确阻断，不静默跳过。

## 工具语言与运行位置

编排器使用 Go，并作为 `engineering/tools` 外部工程工具运行。现有 Manifest Schema、SemVer、打包、local-test Client、Test Release 和多语言构建控制器均以 Go 为稳定组合面；Node 更适合具体前端构建，Python 适合其插件生态，但二者没有足够收益重写跨制品事务。前端构建仍由 Node/esbuild 子工具负责，插件业务语言不受编排器语言限制。

## 影响

- 契约升级由“一处 Registry 配置 + 生成 + 兼容门禁”完成，机械同步遗漏在提交前被发现。
- 插件升级由“一处 Manifest 版本 + 一份 Release Spec”贯穿部署锁、候选构建、测试仓库和热激活。
- 破坏性契约不会因自动替换 Manifest 而伪装成兼容。
- 开发和生产继续共享不可变制品与候选 Generation 语义，但生产权限不会被开发便利扩大。

## 验证

- Contract Registry 生成结果、插件兼容范围和 Foundation 源码硬编码均有测试与统一工程门禁。
- 工作区装载验证全部 Manifest、依赖存在性、SemVer 约束和循环。
- 部署精确引用同步保持原 JSON 布局，只修改匹配插件对象的 version。
- Frontend-only、Backend 和全栈候选使用同一内容寻址 workspace 版本；发布失败保留上一活动 Generation。
