# ADR-0151 最小 Seed 装配与开发插件候选分流

- 状态：已采纳并实施
- 日期：2026-07-25
- 关联：[ADR-0097 测试制品仓库与前端分级热升级](ADR-0097-测试制品仓库与前端分级热升级.md)、[ADR-0142 内核启动与业务发布完全分离](ADR-0142-内核启动与业务发布完全分离.md)、[ADR-0145 本地测试与正式远端制品仓库双协议](ADR-0145-本地测试与正式远端制品仓库双协议.md)、[ADR-0150 插件共享代码与能力复用边界](ADR-0150-插件共享代码与能力复用边界.md)

## 背景

开发编排器虽然已经把普通启动与业务发布分开，但仍在每次准备平台时发现、构建、打包和签署源码树中的全部插件。示例插件、尚未启用的多语言 Runtime 示例、供应链评估插件和 Workbench Gallery 因此被错误地当成 Seed。任意普通插件改动都会扩大前端、Node 和制品仓库构建摘要，并可能触发无关的 stable 身份漂移。

Seed 的职责只是让空环境启动平台管理基础单元，不是本地插件市场的镜像。普通插件已经有 `workspace` 快速候选和 `testing` 完整发布两条受控路径，不需要借平台启动进入仓库。

## 决策

1. 开发 Seed 制品计划只从两个权威输入派生：本地 Backend Seed Platform Profile 中 `services[].plugins` 的精确引用，以及 Portal Platform Catalog 中平台 Profile 的 Runtime Engine、Render Adapter、Shell、Workbench 和 `plugins` 精确引用。
2. 选择器按 Manifest `dependencies` 递归闭包。所有引用必须是 `stable`、版本必须与本地 Manifest 完全一致；同一插件出现不同版本或 channel、依赖约束不满足、插件没有可打包入口时启动前 fail-closed。
3. Backend Kernel 仍是独立构建对象；Go、Node 和 Frontend 插件构建计划只包含所选 Seed 插件。每个 Go 二进制继续使用实际依赖闭包缓存；前端初始构建只生成 Seed Module Graph；Node 构建器只处理所选 Node Worker。
4. 临时 Seed 仓库只打包、签署并写入 Inventory 的精确选择闭包。仓库候选校验和签名前再次比较完整 ref 集合，缺少或额外制品都拒绝。LKG 仍是该 Seed 集合中的安全关键子集。
5. 示例、应用和未启用基础插件不得通过 `bootstrap/up/restart` 隐式进入 Seed。Backend 开发使用 `dev-plugin` 的 `workspace` 候选或 `publish-test` 的 `testing` 候选；Frontend 源码监听仍可发现新插件，但生成的是开发候选，不改变 Seed Inventory 或 stable 仓库。
6. `bootstrap` 仍是显式平台基础发布动作；`up/restart` 只启动并恢复已有期望态。最小 Seed 装配是宿主启动所需的本地输入准备，不授权创建 Deployment、Portal Activation 或普通插件发布事务。

## 备选方案

- 继续把源码树全部插件放入 Seed：操作简单，但启动成本、stable 漂移面和信任面随插件总数增长，否决。
- 维护手写 Seed 插件白名单：短期直接，但会与 Backend/Portal 配置形成第二真相源并遗漏依赖，否决。
- Seed 只包含仓库插件，其他平台插件启动时从 testing 拉取：集合最小，但空环境自举依赖尚未启动的托管仓库并形成循环，否决。
- 普通开发直接覆盖 Seed stable：速度快，但破坏不可变引用、签名证明和回滚，否决。

## 影响

平台启动的构建、签名和扫描成本按实际 Seed 集合而不是仓库插件总数增长；普通插件修改不会再改变 Seed 包缓存或 stable 身份账本。配置新增基础插件时无需同步工具白名单，只需提交精确配置、Manifest 及依赖。

代价是 Seed 配置中的版本必须随插件发布同步提升，配置冲突会更早阻止启动。新普通插件若要进入运行服务，必须走 workspace/testing 及组合发布流程，不能依赖“放进源码目录就随平台启动”。
