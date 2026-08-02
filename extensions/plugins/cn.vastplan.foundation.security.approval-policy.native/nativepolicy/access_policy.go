package nativepolicy

import (
	"context"
	"encoding/json"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
	approvalsdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/approvalpolicy"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const AccessPolicyCapability = "foundation.security.approval-policy.access"

// AccessPolicyContribution keeps the Provider self-governing: installing a
// new implementation also installs the permission decision for its stable
// capability. It authorizes the protocol, not a concrete Provider plugin ID.
func AccessPolicyContribution() sdk.Contribution {
	descriptor, err := json.Marshal(extpoint.CheckerDescriptor{
		Title:   "Approval Provider 插件调用策略",
		Applies: &extpoint.Applies{Target: approvalv2.Capability},
	})
	if err != nil {
		panic(err)
	}
	return sdk.Contribution{
		ExtensionPoint: extpoint.PermissionChecker,
		ID:             AccessPolicyCapability,
		Priority:       1000,
		Descriptor:     descriptor,
		Handlers: map[string]sdk.Handler{"check": func(_ context.Context, _ sdk.Host, call *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
			var request extpoint.PermissionRequest
			response := extpoint.PermissionResponse{Decision: extpoint.DecisionDeny, Reason: "Approval Provider 权限请求无效"}
			if err := json.Unmarshal(raw, &request); err == nil {
				response = approvalsdk.AccessDecision(call, request)
			}
			encoded, err := json.Marshal(response)
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, encoded, err
		}},
	}
}
