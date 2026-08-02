# Native Approval Policy Provider

`cn.vastplan.foundation.security.approval-policy.native` 是第一方 Go `approval.policy.v2` Provider。它读取 service-scoped、版本化 Policy Profiles，以有界声明式规则对可信 actor、resource、operation 和 evidence 求值，并通过通用能力 `foundation.security.approval-policy` 返回决定。

插件不知道 Seed 或企业环境。“禁止自审”“单人复验”“要求角色”等行为全部来自 Profile 数据。业务插件只持有 capability + logical service + routing domain + 精确 ProfileRef，不直接依赖本插件 ID。

安全边界：每内核 Authorization Enforcer 在调用源提供稳定协议的 workload 策略，本插件在目标侧重复贡献同范围检查器；两者允许任意保留可信 Principal 的插件按协议求值，但拒绝用户直连，且不钉死某个消费者或 Provider 插件 ID。Provider 再交叉校验 actor/tenant；Profile 默认效果强制 deny；Profile digest、规则、事实与证据不一致全部拒绝。Provider 只返回决定，不修改业务对象，也不代替 Authorization Enforcer。

当前使用 Go 是因为该能力安全关键、低延迟且属于确定性纯计算。未来 OPA、Cedar 或企业外部审批 Provider 可以使用其他语言和独立进程实现同一协议。

企业“禁止自审”不是环境硬编码，而是一份普通 Profile：

```json
{
  "id": "enterprise.portal-publication",
  "revision": 1,
  "defaultEffect": "deny",
  "rules": [
    {
      "id": "enterprise.portal-publication.deny-self",
      "priority": 100,
      "conditions": [{ "left": "actor.id", "operator": "equals", "rightFact": "resource.submittedBy" }],
      "effect": "deny",
      "code": "approval.separation_required",
      "message": "提交人不能审批自己的内容"
    },
    {
      "id": "enterprise.portal-publication.allow-other",
      "priority": 90,
      "conditions": [{ "left": "actor.id", "operator": "not-equals", "rightFact": "resource.submittedBy" }],
      "effect": "allow"
    }
  ]
}
```

部署把 Composer 的 `profile` 精确绑定到上述 Profile 的 `id/revision/digest` 即可；更换规则不需要修改 Portal Composer 或内核。
