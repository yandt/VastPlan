package approval

import (
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
	approvalsdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/approvalpolicy"
)

func TestPolicyAllowsProtocolConsumersWithoutPluginIDAllowlist(t *testing.T) {
	request := extpoint.PermissionRequest{ExtensionPoint: extpoint.ToolPackage, Capability: approvalv2.Capability, Operation: "evaluate"}
	for _, pluginID := range []string{"cn.vastplan.platform.configuration.portal-composer", "cn.example.future.workflow"} {
		call := &contractv1.CallContext{TenantId: "local", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: pluginID}, Principal: &contractv1.Principal{UserId: "approver"}}
		if got := approvalsdk.AccessDecision(call, request); got.Decision != extpoint.DecisionAllow {
			t.Fatalf("稳定协议不应维护消费者插件 allowlist: plugin=%s decision=%+v", pluginID, got)
		}
	}
	user := &contractv1.CallContext{TenantId: "local", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "approver"}, Principal: &contractv1.Principal{UserId: "approver"}}
	if got := approvalsdk.AccessDecision(user, request); got.Decision != extpoint.DecisionDeny {
		t.Fatalf("用户不得直连 Approval Provider: %+v", got)
	}
}
