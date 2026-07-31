# ADR-0159：Ant Design 首选 Renderer 与按需交付

- 状态：Accepted
- 日期：2026-07-26
- 修订：补充 ADR-0087，不覆盖其统一 Adapter 决策

## 背景

Portal 已以 React Runtime Engine、统一 Render Adapter、Shell 和 Workbench 分层。Arco 与 MUI 验证了同一语义 UI Contract 可映射到不同框架，但平台管理中心需要将 Ant Design 作为首选视觉实现，同时不能让功能插件获得框架依赖，也不能使未选择的框架增加首屏下载。

## 决策

1. React 继续是唯一已发布的 Runtime Engine。Ant Design 是统一 `cn.vastplan.foundation.frontend.render.adapter` Catalog 中第三个受信任 Renderer，ID 为 `antd`。
2. 新的本地 Platform Profile 默认 `defaultRenderer=antd`，并按 `antd、arco、mui` 顺序允许选择。Arco 与 MUI 保留为可切换备选，不迁移业务页面代码。
3. Ant Design Renderer 独立发布为 `cn.vastplan.foundation.frontend.render.adapter.antd`。统一 Adapter 只保存精确模块引用，不静态导入任何 UI 框架；Portal 在确定 Profile 和用户偏好后只校验、下载、导入一个 Renderer。
4. 功能插件继续只声明 Workbench 和框架无关 UI 契约。工程门禁禁止功能插件直接导入 `antd`、`@ant-design/*`、Arco、MUI 或 React。
5. Renderer 使用 Ant Design 原生 `ConfigProvider`、明暗算法、locale 和图标；CSS-in-JS 样式及 Overlay 容器必须落在 Portal Shadow Root 内。图标必须使用逐个子路径入口，构建时校验数量与 bundle 体积。
6. 动态表单复用共享 CSP JSON Schema Validator。生产入口直接使用 RJSF Form 子路径，只选择安全的 Ant Design widgets/templates，不允许根入口间接引入测试 Registry 或运行时代码生成。
7. Renderer 切换仍是 Host Epoch 边界：先验证目标精确制品，再刷新并装配新 Generation；同一 React 树不得同时承载两个 Renderer。

2026-08-01 修订：第 3 条中“Adapter 保存精确模块引用”的实现方式已由 [ADR-0180](ADR-0180-Catalog驱动的统一插件自发现与本地插件库.md) 取代。Adapter 现在只保存选择语义、默认值和本地化；精确 Ant Design 模块引用由可信宿主从已验证 Manifest Contribution Index 与 Platform Profile 解析锁派生，按需加载和 Host Epoch 边界不变。

## 备选方案

- 把 Ant Design 做成第二个完整 Adapter：会复制 Catalog、Profile、偏好和切换逻辑，破坏统一治理，拒绝。
- 用 Ant Design 直接替换 Arco/MUI：会失去多 Renderer 契约验证和已有备选样式，拒绝。
- 将 Ant Design 并入 React Runtime Engine：会把执行引擎与视觉实现重新耦合，拒绝。

## 后果

首选界面可使用 Ant Design 生态和原生主题能力，既有功能插件无需修改。仓库会增加一个较大的基础 Renderer 制品，但它是延迟模块，未选中时不进入 Portal 首屏；其体积和依赖扩张由构建门禁约束。
