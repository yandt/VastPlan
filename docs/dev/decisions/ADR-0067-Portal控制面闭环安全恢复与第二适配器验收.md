# ADR-0067 Portal 控制面闭环、安全恢复与第二适配器验收

- 状态：已接受
- 日期：2026-07-18
- 关联：[ADR-0052 前端门户内核与多 UI 设计系统插件](ADR-0052-前端门户内核与多UI设计系统插件.md)、[ADR-0059 Frontend 双输入采用服务端权威解析](ADR-0059-Frontend双输入服务端权威解析.md)、[ADR-0062 Frontend 可信 ESM 制品与运行描述](ADR-0062-Frontend可信ESM制品与运行描述.md)

## 背景

Portal 已具备双输入解析、可信 ESM 装载、Arco 设计系统和受治理的发布状态机，但仍缺少三个可交付闭环：草稿只能创建不能继续编辑；设计系统或功能模块启动失败后只有静态错误页；多 UI 框架仍停留在接口预留，没有第二个真实适配器证明普通功能插件与 Arco 解耦。

恢复链路尤其不能由浏览器直接指定任意历史 revision。否则攻击者可绕过当前发布状态，主动加载已撤销、存在漏洞或不再符合平台基线的前端制品。

## 决策

1. `@vastplan/ui-primitives` 提供类型化 `PortalControlClient`，覆盖 list、create、update、submit、approve、publish、rollback 和 audit。每次非安全读取操作重新取得短期 CSRF token；错误向 UI 暴露稳定错误码，不暴露内部调用细节。
2. Composer 的草稿允许在 `draft` 状态更新完整 Application Composition；提交后不可编辑。更新仍执行 Schema、插件分类、Catalog 和当前 Platform Profile 解析校验，并产生 `draft.updated` 审计事件。
3. Portal Composer 页面提供 revision 列表、草稿创建/编辑、差异预览、提交、审批、发布、回滚和审计查看。职责分离和最终权限仍由服务端策略裁决，按钮可见性不是授权边界。
4. 普通活动模块继续只通过 `/v1/portal-modules/{activeRevision}/{plugin}.js` 读取。启动失败时，内核原生恢复页可请求 `/v1/portal-recovery`；服务端只选择同租户、同 Portal ID、非当前且最近的已发布 revision。
5. 恢复模块 URL 同时绑定当前活动 revision 与服务端选择的回退 revision：`/v1/portal-recovery-modules/{active}/{fallback}/{plugin}.js`。读取时再次验证当前活动版本未变化、fallback 仍是最新合法候选并重新验签制品。浏览器不能传入版本、channel 或任意包路径。
6. 恢复版本只用于临时安全模式，页面明确标识正在运行旧 revision。它不改变服务端活动状态；管理员必须通过正式回滚或发布修复版本完成收敛。
7. 增加 `cn.vastplan.foundation.frontend.render.adapter.mui`，以 Material UI 实现同一 `@vastplan/ui-primitives` 1.x 组件面。Portal Composer 删除对 Arco 的插件依赖，继续只依赖公共 UI SDK。Arco 与 MUI 分别构建为独立单文件 ESM，同一 Portal 仍只能选择一个设计系统。

## 被否决方案

- **浏览器从历史列表任选恢复版本**：操作灵活，但把发布状态与漏洞撤销权交给客户端，否决。
- **启动失败时自动修改活动 revision**：恢复迅速，但一次浏览器故障会变成控制面写操作且绕过审批，否决。
- **把 MUI 作为测试桩而不交付插件**：只能证明 TypeScript 形状，不能证明依赖、构建和运行边界，否决。
- **Portal Composer 依赖 Arco 插件**：会使普通功能插件无法用于 MUI Portal，违反框架无关边界，删除该依赖。

## 结果与验收

- 正面：在线组合形成完整可操作状态机；恢复选择和模块字节仍由服务端权威治理；第二套真实设计系统证明功能插件没有绑定 Arco。
- 代价：Edge 增加只读恢复端点；MUI 作为独立制品引入自己的框架体积；安全模式只保证可访问旧界面，不替代正式控制面回滚。
- 验收：Go 服务/策略/Edge 单元测试、真实 Portal Edge 进程 E2E、Portal Kernel/Composer/MUI Vitest、全仓 TypeScript 类型检查和三类前端 ESM 构建必须同时通过。
