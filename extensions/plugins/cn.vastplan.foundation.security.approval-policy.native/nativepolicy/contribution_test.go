package nativepolicy

import (
	"context"
	"encoding/json"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
	approvalsdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/approvalpolicy"
)

func TestContributionAcceptsOnlyTrustedPluginFacts(t *testing.T) {
	provider, ref := buildProvider(t, enterprisePolicy())
	payload, err := json.Marshal(approvalv2.EvaluateRequest{Profile: ref, Input: input("approver", "submitter")})
	if err != nil {
		t.Fatal(err)
	}
	handler := provider.Contribution().Handlers["evaluate"]

	userCall := trustedApprovalCall("approver")
	userCall.Caller.Kind = contractv1.CallerKind_CALLER_KIND_USER
	result, _, err := handler(context.Background(), nil, userCall, payload)
	if err != nil || result.GetError().GetCode() != "approval.provider.forbidden" {
		t.Fatalf("直接用户调用必须被拒绝: result=%+v err=%v", result, err)
	}

	forgedCall := trustedApprovalCall("different-user")
	result, _, err = handler(context.Background(), nil, forgedCall, payload)
	if err != nil || result.GetError().GetCode() != "approval.provider.forbidden" {
		t.Fatalf("与 CallContext 不一致的 actor 必须被拒绝: result=%+v err=%v", result, err)
	}

	result, raw, err := handler(context.Background(), nil, trustedApprovalCall("approver"), payload)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || len(raw) == 0 {
		t.Fatalf("可信插件调用应成功: result=%+v raw=%s err=%v", result, raw, err)
	}
}

func TestAccessPolicyGovernsProtocolWithoutPinningConsumerPlugin(t *testing.T) {
	raw, _ := json.Marshal(extpoint.PermissionRequest{ExtensionPoint: extpoint.ToolPackage, Capability: approvalv2.Capability, Operation: "evaluate"})
	for _, pluginID := range []string{"cn.vastplan.platform.configuration.portal-composer", "cn.example.future.workflow"} {
		call := trustedApprovalCall("approver")
		call.Caller.Id = pluginID
		var request extpoint.PermissionRequest
		_ = json.Unmarshal(raw, &request)
		if got := approvalsdk.AccessDecision(call, request); got.Decision != extpoint.DecisionAllow {
			t.Fatalf("任意受信插件应通过稳定 Provider 协议组合: plugin=%s decision=%+v", pluginID, got)
		}
	}
	user := trustedApprovalCall("approver")
	user.Caller.Kind = contractv1.CallerKind_CALLER_KIND_USER
	var request extpoint.PermissionRequest
	_ = json.Unmarshal(raw, &request)
	if got := approvalsdk.AccessDecision(user, request); got.Decision != extpoint.DecisionDeny {
		t.Fatalf("浏览器用户不得直接调用 Provider: %+v", got)
	}
	other, _ := json.Marshal(extpoint.PermissionRequest{ExtensionPoint: extpoint.ToolPackage, Capability: "platform.other", Operation: "evaluate"})
	_ = json.Unmarshal(other, &request)
	if got := approvalsdk.AccessDecision(trustedApprovalCall("approver"), request); got.Decision != extpoint.DecisionAbstain {
		t.Fatalf("Provider 策略不得接管其他能力: %+v", got)
	}
}

func trustedApprovalCall(userID string) *contractv1.CallContext {
	return &contractv1.CallContext{
		TenantId: "local",
		Caller: &contractv1.Caller{
			Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN,
			Id:   "cn.vastplan.test.approval-caller",
		},
		Principal: &contractv1.Principal{UserId: userID},
	}
}
