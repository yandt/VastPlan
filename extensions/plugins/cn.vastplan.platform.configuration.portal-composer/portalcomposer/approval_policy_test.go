package portalcomposer

import (
	"context"
	"encoding/json"
	"strings"

	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
)

type testApprovalPolicy struct {
	evaluate func(approvalv2.EvaluationInput) approvalv2.Decision
}

func (p testApprovalPolicy) Evaluate(_ context.Context, input approvalv2.EvaluationInput) (approvalv2.Decision, error) {
	return p.evaluate(input), nil
}

func (p testApprovalPolicy) EvaluateBatch(_ context.Context, inputs []approvalv2.EvaluationInput) ([]approvalv2.Decision, error) {
	result := make([]approvalv2.Decision, 0, len(inputs))
	for _, input := range inputs {
		result = append(result, p.evaluate(input))
	}
	return result, nil
}

func withDifferentSubjectTestPolicy(ctx context.Context) context.Context {
	ref := approvalv2.ProfileRef{ID: "test.different-subject", Revision: 1, Digest: strings.Repeat("a", 64)}
	return withApprovalPolicy(ctx, testApprovalPolicy{evaluate: func(input approvalv2.EvaluationInput) approvalv2.Decision {
		if input.Actor.ID == input.Resource.SubmittedBy {
			return approvalv2.Decision{Status: approvalv2.DecisionDenied, Profile: ref, MatchedRuleID: "test.deny-self", Code: "approval.separation_required", Message: "提交人不能审批自己的内容"}
		}
		return approvalv2.Decision{Status: approvalv2.DecisionAllowed, Profile: ref, MatchedRuleID: "test.allow-other", AuditNote: "rule=test.allow-other"}
	}})
}

func differentSubjectTestContext() context.Context {
	return withDifferentSubjectTestPolicy(context.Background())
}

func testApprovalBinding() approvalv2.ProviderBinding {
	return approvalv2.ProviderBinding{
		Protocol:       approvalv2.Protocol,
		Capability:     approvalv2.Capability,
		LogicalService: "test.approval-policy",
		RoutingDomain:  "security",
		Profile: approvalv2.ProfileRef{
			ID: "test.different-subject", Revision: 1, Digest: strings.Repeat("a", 64),
		},
	}
}

func newBoundTestService() *Service {
	return NewWithApprovalBinding(nil, testApprovalBinding())
}

func withSeedReviewTestPolicy(ctx context.Context) context.Context {
	ref := approvalv2.ProfileRef{ID: "test.seed-review", Revision: 1, Digest: strings.Repeat("b", 64)}
	requirements := []approvalv2.EvidenceRequirement{
		{ID: "test.digest", Field: "review.expected-digest", Kind: approvalv2.EvidenceExactFactMatch, Fact: "resource.digest"},
		{ID: "test.acknowledged", Field: "review.acknowledged", Kind: approvalv2.EvidenceBooleanTrue},
		{ID: "test.reason", Field: "review.reason", Kind: approvalv2.EvidenceTextLength, MinLength: 4, MaxLength: 512},
	}
	return withApprovalPolicy(ctx, testApprovalPolicy{evaluate: func(input approvalv2.EvaluationInput) approvalv2.Decision {
		if input.Actor.ID != input.Resource.SubmittedBy {
			return approvalv2.Decision{Status: approvalv2.DecisionAllowed, Profile: ref, MatchedRuleID: "test.allow-other"}
		}
		if len(input.Evidence) == 0 {
			return approvalv2.Decision{Status: approvalv2.DecisionReviewRequired, Profile: ref, MatchedRuleID: "test.self-review", Requirements: requirements, Message: "需复验"}
		}
		var digest string
		var acknowledged bool
		var reason string
		_ = json.Unmarshal(input.Evidence["review.expected-digest"], &digest)
		_ = json.Unmarshal(input.Evidence["review.acknowledged"], &acknowledged)
		_ = json.Unmarshal(input.Evidence["review.reason"], &reason)
		if digest != input.Resource.Digest || !acknowledged || len(strings.TrimSpace(reason)) < 4 {
			return approvalv2.Decision{Status: approvalv2.DecisionDenied, Profile: ref, MatchedRuleID: "test.self-review", Code: "approval.evidence.mismatch", Message: "证据不一致"}
		}
		return approvalv2.Decision{Status: approvalv2.DecisionAllowed, Profile: ref, MatchedRuleID: "test.self-review", AuditNote: "rule=test.self-review review.reason=" + reason}
	}})
}
