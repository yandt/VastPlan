# ADR-0181 权限 Glob 选择器与 Catalog 绑定编译

- 状态：已采纳
- 日期：2026-08-01

## 背景

在线角色当前只能逐条保存精确权限码。平台 Owner 和按功能域授权需要重复枚举大量权限，Permission Catalog 增长后还容易漏配。直接让 Enforcer、Portal Session 或多语言 Engine 在运行期解释通配符或正则，会让旧角色自动获得未来插件新增权限，并造成多实现语义漂移。

## 决策

Authorization Policy 的角色治理输入增加带类型的权限选择器：`exact` 保存精确权限码，`glob` 使用受限的点分段语法。`*` 只匹配一个权限段，`**` 匹配一个或多个权限段；首段必须是字面命名空间，禁止任意正则、部分段通配和运行期表达式。

角色创建或更新时，Policy 服务只在当前签名 Permission Catalog、当前 Domain delegation ceiling、风险上限和 `assignable=true` 的交集中展开选择器。每个选择器必须至少匹配一个权限，结果去重并稳定排序，同时保存选择器、Catalog digest 与展开后的精确权限。

已发布 Role revision、Authorization IR、Policy Snapshot、Native/第三方 Engine、Authorization Session、Portal BFF 和 Runner/Mobile 投影只消费展开后的精确权限码。Catalog 后续新增权限不会改变旧 Role revision；需要扩大授权时必须形成新的角色 revision，重新审批并发布。显式 deny、撤权和领域状态规则继续优先。

开发 Seed Owner 使用受信 Glob 选择器生成当前 Catalog 的管理权限。显式 `bootstrap` 由组合根选择一次性 `seed-owned` 协调策略，将本地 Bootstrap State 中的 Seed Authority 对象原子同步进权威 Shared State；它可以补建缺失 Role/Binding、收敛旧 Seed 权限并删除不再属于当前基线的 Seed 对象。协调提交时必须从完整 Shared State 重新编译和签发 Snapshot，不能让本地 Seed 文件覆盖在线创建的用户角色。同名 ID 被非受信定义占用、revision 异常或 Binding 失去精确 Role 引用时必须失败。普通 `up/restart` 注入禁用策略，不创建新授权对象。

## 备选方案

- 运行期 Glob：实现表面简单，但旧策略会自动覆盖未来权限，且所有强制点必须复制匹配器，拒绝。
- RE2 正则后编译：可避免回溯型拒绝服务，但表达难以审计、跨语言管理体验差，当前不采用。
- Glob 与正则同时支持：扩展性最高，但在现阶段增加不必要的协议和测试分支，暂缓。

## 影响

- 角色治理模型需要同时保存选择器意图、Catalog digest 和精确展开结果。
- Authorization IR v1 和全部运行期判定协议保持不变，性能仍为精确集合查询。
- Permission Catalog 变化不会静默扩大已发布角色；管理员能在新 revision 中看到准确权限差异。
- 开发 Seed 初始化先后顺序不再导致稳定主体缺少 Owner Binding。
- Bootstrap 文件、签名 Snapshot 与在线 Shared State 不再形成两套 Seed 授权真相；协调只触及 `seed-authority` 所有权对象并留下独立审计。
