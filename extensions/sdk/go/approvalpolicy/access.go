package approvalpolicy

import (
	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
)

// AccessDecision is the canonical workload rule used at both the calling
// kernel and the Provider target. Keeping it in the protocol SDK prevents the
// two enforcement points from drifting.
func AccessDecision(call *contractv1.CallContext, request extpoint.PermissionRequest) extpoint.PermissionResponse {
	if request.ExtensionPoint != extpoint.ToolPackage || request.Capability != approvalv2.Capability {
		return extpoint.PermissionResponse{Decision: extpoint.DecisionAbstain, Reason: "非 Approval Provider 调用"}
	}
	caller := call.GetCaller()
	if request.Operation == "health" && caller.GetId() != "" && (caller.GetKind() == contractv1.CallerKind_CALLER_KIND_PLUGIN || caller.GetKind() == contractv1.CallerKind_CALLER_KIND_SYSTEM) {
		return extpoint.PermissionResponse{Decision: extpoint.DecisionAllow, Reason: "可信 workload 读取 Approval Provider 健康状态"}
	}
	if request.Operation != "evaluate" && request.Operation != "evaluateBatch" {
		return extpoint.PermissionResponse{Decision: extpoint.DecisionDeny, Reason: "未知 Approval Provider 操作"}
	}
	if caller.GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || caller.GetId() == "" || call.GetPrincipal().GetUserId() == "" || call.GetTenantId() == "" {
		return extpoint.PermissionResponse{Decision: extpoint.DecisionDeny, Reason: "Approval Provider 只接受保留可信主体的插件调用"}
	}
	return extpoint.PermissionResponse{Decision: extpoint.DecisionAllow, Reason: "受信插件调用通用 Approval Provider 协议"}
}
