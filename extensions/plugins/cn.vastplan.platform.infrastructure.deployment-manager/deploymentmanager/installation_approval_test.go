package deploymentmanager

import (
	"context"
	"strings"
	"testing"

	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
)

func TestSelfServiceAllowedPolicyAutoApprovesThroughSameCandidateStateMachine(t *testing.T) {
	service, host, call := publishedIntentService(t)
	call.Scene = "portal.bff"
	ref := approvalv2.ProfileRef{ID: "enterprise.plugin-installation", Revision: 3, Digest: strings.Repeat("a", 64)}
	binding := approvalv2.ProviderBinding{Protocol: approvalv2.Protocol, Capability: approvalv2.Capability, LogicalService: "foundation.approval-policy.native", RoutingDomain: "security", Profile: ref}
	service.approvalBinding = &binding
	host.approvalDecision = &approvalv2.Decision{Status: approvalv2.DecisionAllowed, Profile: ref, MatchedRuleID: "first-party.direct"}
	request := approvalPreviewRequest()
	preview, err := service.PreviewPluginInstallation(context.Background(), host, call, plugininstallation.SourceSelfService, request)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Impact.RequiresApproval || preview.Approval == nil || preview.Approval.Status != approvalv2.DecisionAllowed {
		t.Fatalf("允许策略没有投影到预览: %+v", preview)
	}
	candidate, err := service.CreatePluginInstallationCandidate(context.Background(), host, call, plugininstallation.SourceSelfService, request)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err = service.SubmitSelfServicePluginInstallationCandidate(context.Background(), host, call, candidate.ID, request.Target)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Status != plugininstallation.CandidateApproved || candidate.ApprovedBy != "policy:enterprise.plugin-installation@3" {
		t.Fatalf("免人工审批没有复用 Approved 状态: %+v", candidate)
	}
}

func TestSelfServiceReviewPolicyRemainsPendingForIndependentApproval(t *testing.T) {
	service, host, call := publishedIntentService(t)
	call.Scene = "portal.bff"
	ref := approvalv2.ProfileRef{ID: "enterprise.plugin-installation", Revision: 3, Digest: strings.Repeat("b", 64)}
	service.approvalBinding = &approvalv2.ProviderBinding{Protocol: approvalv2.Protocol, Capability: approvalv2.Capability, LogicalService: "foundation.approval-policy.native", RoutingDomain: "security", Profile: ref}
	host.approvalDecision = &approvalv2.Decision{Status: approvalv2.DecisionReviewRequired, Profile: ref, MatchedRuleID: "third-party.review", Requirements: []approvalv2.EvidenceRequirement{{ID: "installation.review.reason", Field: "review.reason", Kind: approvalv2.EvidenceTextLength, MinLength: 4, MaxLength: 512}}}
	request := approvalPreviewRequest()
	candidate, err := service.CreatePluginInstallationCandidate(context.Background(), host, call, plugininstallation.SourceSelfService, request)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err = service.SubmitSelfServicePluginInstallationCandidate(context.Background(), host, call, candidate.ID, request.Target)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Status != plugininstallation.CandidatePendingApproval || candidate.Preview.Approval == nil {
		t.Fatalf("需审批策略没有保留候选审批态: %+v", candidate)
	}
}

func approvalPreviewRequest() plugininstallation.PreviewRequest {
	return plugininstallation.PreviewRequest{Version: 1, Target: plugininstallation.Target{Kernel: "backend", Deployment: "agent-services", UnitID: "api"}, PortalTargets: []string{"operations"}, Change: plugininstallation.Change{
		Action: plugininstallation.ActionUpgrade, PluginID: "cn.vastplan.product.agent.api", Requirement: &pluginv1.ArtifactRequirement{PluginID: "cn.vastplan.product.agent.api", Constraint: "=2.0.0", Channel: "stable"},
	}}
}
