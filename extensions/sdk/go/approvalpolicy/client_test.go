package approvalpolicy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
)

type fakeHost struct {
	target *contractv1.CallTarget
	result *contractv1.CallResult
	raw    []byte
}

func (h *fakeHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
	h.target = target
	return h.result, h.raw, nil
}

func TestClientPinsProviderTargetAndProfile(t *testing.T) {
	binding := testBinding()
	decision := approvalv2.Decision{Status: approvalv2.DecisionAllowed, Profile: binding.Profile, MatchedRuleID: "seed.other-actor"}
	raw, _ := json.Marshal(approvalv2.EvaluateResult{Decision: decision})
	host := &fakeHost{result: &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw: raw}
	client, err := New(host, binding)
	if err != nil {
		t.Fatal(err)
	}
	call := &contractv1.CallContext{TenantId: "local", Principal: &contractv1.Principal{UserId: "operator"}}
	input := approvalv2.EvaluationInput{Operation: "portal.publication.approve", TenantID: "local", Actor: approvalv2.ActorFacts{ID: "operator"}, Resource: approvalv2.ResourceFacts{ID: "operations/1", Digest: strings.Repeat("a", 64)}}
	if _, err := client.Evaluate(context.Background(), call, input); err != nil {
		t.Fatal(err)
	}
	if host.target.GetCapability() != approvalv2.Capability || host.target.GetLogicalService() != binding.LogicalService || host.target.GetRoutingDomain() != binding.RoutingDomain {
		t.Fatalf("Provider 寻址未钉死: %+v", host.target)
	}
}

func TestClientRejectsFactsOutsideTrustedContext(t *testing.T) {
	binding := testBinding()
	host := &fakeHost{}
	client, err := New(host, binding)
	if err != nil {
		t.Fatal(err)
	}
	call := &contractv1.CallContext{TenantId: "local", Principal: &contractv1.Principal{UserId: "operator"}}
	input := approvalv2.EvaluationInput{Operation: "portal.publication.approve", TenantID: "other", Actor: approvalv2.ActorFacts{ID: "forged"}, Resource: approvalv2.ResourceFacts{ID: "operations/1", Digest: strings.Repeat("a", 64)}}
	if _, err := client.Evaluate(context.Background(), call, input); err == nil {
		t.Fatal("SDK 必须在调用 Provider 前拒绝伪造 actor/tenant")
	}
	if host.target != nil {
		t.Fatal("无效事实不得发出 Provider 调用")
	}
}

func TestAccessDecisionIsConsumerAndProviderIndependent(t *testing.T) {
	request := extpoint.PermissionRequest{ExtensionPoint: extpoint.ToolPackage, Capability: approvalv2.Capability, Operation: "evaluate"}
	for _, pluginID := range []string{"cn.vastplan.platform.configuration.portal-composer", "cn.example.future.workflow"} {
		call := &contractv1.CallContext{TenantId: "local", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: pluginID}, Principal: &contractv1.Principal{UserId: "approver"}}
		if got := AccessDecision(call, request); got.Decision != extpoint.DecisionAllow {
			t.Fatalf("稳定协议不应维护消费者或 Provider 插件 allowlist: plugin=%s decision=%+v", pluginID, got)
		}
	}
}

func testBinding() approvalv2.ProviderBinding {
	return approvalv2.ProviderBinding{Protocol: approvalv2.Protocol, Capability: approvalv2.Capability, LogicalService: "foundation.approval-policy.native", RoutingDomain: "security", Profile: approvalv2.ProfileRef{ID: "seed.portal-publication", Revision: 1, Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
}
