# Native Approval Policy Provider

插件 ID：`cn.vastplan.foundation.security.approval-policy.native`
当前版本：`0.1.2`

该 Foundation Provider 实现 `approval.policy.v2` 与通用能力 `foundation.security.approval-policy`。它拥有独立配置、版本、替换和故障边界，但不拥有 Portal、Deployment 等业务对象；调用方仍是最终状态转换强制点。

Provider 从 service-scoped 配置加载一个或多个版本化 Policy Profile。Profile 固定 deny 默认效果，并用受限事实、通用操作符、规则优先级、效果和证据要求表达审批逻辑。业务服务通过 Provider Binding 选择精确 Profile digest，不引用插件 ID。

当前 Native Provider 可进入可信共享 Go Runtime。第三方或外部 Provider 可以通过相同 capability 协议运行在独立进程；其隔离级别继续由内核使用者和发布者策略决定。完整决策见 [ADR-0189](../decisions/ADR-0189-审批策略Provider与声明式规则.md)。
