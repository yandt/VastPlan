# ADR-0189 审批策略 Provider 与声明式规则

- 状态：已采纳，首个 Native Provider 已实施
- 日期：2026-08-02
- 关联：[ADR-0188](ADR-0188-授权与业务审批策略解耦.md)、[ADR-0170](ADR-0170-统一Capability-Contract与可信ArtifactIdentity.md)

## 背景

ADR-0188 已把 Authorization 与业务 Approval 分离，但 `approval.policy.v1` 仍让 Portal Composer 进程内的 Go Adapter 根据 `different-subject/single-operator-review` 两个枚举执行固定业务分支。这只能做到“模式可配置”，无法做到“规则与实现均可替换”，也会让新增人数门槛、角色、组织关系、外部审批系统或第三方 Policy Engine 时继续修改业务插件。

系统不应判断 Seed、开发或企业环境。部署形态只选择一份 Provider Binding 和一个精确 Policy Profile；同一 Seed 可以选择严格异人审批，同一企业也可以在其治理允许时选择单人复验。

## 决策

以不兼容升级的 `approval.policy.v2` 替代 v1：

1. 业务插件只调用稳定能力 `foundation.security.approval-policy/evaluate`，不解释 Provider 类型和规则。
2. `ProviderBinding` 以 capability、logical service、routing domain 和精确 `profileRef(id/revision/digest)` 选择实现；不直接向业务配置暴露插件 ID。
3. Native Provider 接受版本化声明式 Profile。规则由有界事实、通用比较操作符、优先级、效果和证据要求组成；“禁止自审”只是普通 Rule，不是环境分支。
4. Provider 返回 `allowed/review-required/denied`、精确 ProfileRef、命中规则和证据要求。业务插件仍拥有对象状态与最终状态转换，不允许 Provider 修改对象。
5. Provider 不可用、超时、返回不兼容结果、Profile 摘要不一致或规则无匹配时全部 fail-closed。
6. Policy Profile 的候选更新不得批准自己生效。后续在线管理必须以当前活动 Profile 审批候选摘要，再原子切换新 Generation；本 ADR 首期通过受治理的 service-scoped 配置交付 Profile，保留同一协议的在线控制面接入口。

## 声明式边界

首版事实包括 operation、actor id/roles、resource id/digest/submittedBy、tenant 与命名空间化 attributes。Native Provider 固定实现 `equals/not-equals/contains/not-contains/exists/not-exists` 等通用操作符，以及 `allow/deny/require-evidence` 三种效果。固定的是安全解释器和可信事实来源，不是 Portal 的审批业务规则。

证据要求也是规则数据。Seed Profile 通过 `require-evidence` 要求冻结摘要确认、布尔确认和审计原因；企业 Profile 可以通过 `deny` 规则拒绝 `actor.id == resource.submittedBy`。所有决定记录 Profile digest 和 matched rule，避免审计只看到模糊模式名。

## 插件与语言边界

第一方 `cn.vastplan.foundation.security.approval-policy.native` 使用 Go：规则求值是安全关键、低延迟、确定性的纯计算，Go 与现有 CallContext、Provider SDK、集群寻址和制品链最匹配。它作为真实 Foundation Provider 插件拥有独立替换、版本、配置和故障边界，可进入可信共享 Go Runtime；不为每条规则创建插件或进程。

未来 OPA/Rego Provider 优先使用 Go 生态，Cedar Provider 可使用 Rust 独立进程，企业现有审批系统可由 Java、Node、Python 等实现同一 RPC 契约。第三方 Provider 的进程隔离仍由内核使用者和发布者策略配置决定。

## 安全边界

- HostCall 必须证明调用方是插件，并保留可信 Principal；Provider 交叉校验 actor 和 tenant，不接受浏览器伪造主体。
- 每内核 Authorization Enforcer 在调用源提供只匹配稳定 Approval capability 的 workload 策略，Provider 在目标侧重复贡献同范围检查器：任意保留可信 Principal 的插件可按协议求值，用户不能直连；两侧授权均不钉死某个业务插件或 Provider 插件 ID。
- Native Profile 默认效果必须为 deny；配置、事实、操作符或证据非法均拒绝启动或拒绝决定。
- ProfileRef 必须使用精确 revision 与 SHA-256 digest；Provider 返回的引用必须与 Binding 完全一致。
- 读模型投影可批量求值，但写操作必须在对象转换前重新求值，并在返回后重新确认冻结摘要和状态，关闭 TOCTOU 窗口。
- 权限资格继续由 Authorization Enforcer 判断；Approval Provider 不读取 Role Store、不签发 Session，也不能把审批决定当作权限证明。

## 影响

删除 Portal Composer 对固定审批模式的依赖。部署增加一个可替换 Approval Provider 服务单元，并让 Composer 通过强依赖等待其 readiness。当前前端仍消费统一决定和数据驱动证据要求；更换 Provider 或 Profile 不改变 Workbench 页面和 Portal API。
