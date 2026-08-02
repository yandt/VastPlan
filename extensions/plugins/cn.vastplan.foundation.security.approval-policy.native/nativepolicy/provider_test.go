package nativepolicy

import (
	"encoding/json"
	"strings"
	"testing"

	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
)

func TestRulesConfigureSeedAndEnterpriseBehavior(t *testing.T) {
	seed, seedRef := buildProvider(t, seedPolicy())
	seedInput := input("operator", "operator")
	decision, err := seed.Evaluate(approvalv2.EvaluateRequest{Profile: seedRef, Input: seedInput})
	if err != nil || decision.Status != approvalv2.DecisionReviewRequired || decision.MatchedRuleID != "seed.self-review" {
		t.Fatalf("Seed 自审应由规则要求证据: %+v err=%v", decision, err)
	}
	seedInput.Evidence = evidence(seedInput.Resource.Digest, "已复核冻结配置")
	decision, err = seed.Evaluate(approvalv2.EvaluateRequest{Profile: seedRef, Input: seedInput})
	if err != nil || decision.Status != approvalv2.DecisionAllowed || !strings.Contains(decision.AuditNote, "review.reason=已复核冻结配置") {
		t.Fatalf("Seed 证据完成后应允许: %+v err=%v", decision, err)
	}

	enterprise, enterpriseRef := buildProvider(t, enterprisePolicy())
	decision, err = enterprise.Evaluate(approvalv2.EvaluateRequest{Profile: enterpriseRef, Input: input("submitter", "submitter")})
	if err != nil || decision.Status != approvalv2.DecisionDenied || decision.MatchedRuleID != "enterprise.deny-self" {
		t.Fatalf("企业自审拒绝应来自声明式规则: %+v err=%v", decision, err)
	}
	decision, err = enterprise.Evaluate(approvalv2.EvaluateRequest{Profile: enterpriseRef, Input: input("approver", "submitter")})
	if err != nil || decision.Status != approvalv2.DecisionAllowed || decision.MatchedRuleID != "enterprise.allow-other" {
		t.Fatalf("企业异人审批允许应来自声明式规则: %+v err=%v", decision, err)
	}
}

func TestEvidenceMismatchFailsClosed(t *testing.T) {
	provider, ref := buildProvider(t, seedPolicy())
	value := input("operator", "operator")
	value.Evidence = evidence(strings.Repeat("f", 64), "已复核冻结配置")
	decision, err := provider.Evaluate(approvalv2.EvaluateRequest{Profile: ref, Input: value})
	if err != nil || decision.Status != approvalv2.DecisionDenied || decision.Code != "approval.evidence.mismatch" {
		t.Fatalf("错误摘要必须 fail-closed: %+v err=%v", decision, err)
	}
}

func buildProvider(t *testing.T, profile approvalv2.PolicyProfile) (*Provider, approvalv2.ProfileRef) {
	t.Helper()
	provider, err := New([]approvalv2.PolicyProfile{profile})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := approvalv2.RefForProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	return provider, ref
}

func seedPolicy() approvalv2.PolicyProfile {
	return approvalv2.PolicyProfile{ID: "seed.portal-publication", Revision: 1, DefaultEffect: approvalv2.EffectDeny, Rules: []approvalv2.Rule{
		{ID: "seed.self-review", Priority: 100, Conditions: []approvalv2.Condition{{Left: "actor.id", Operator: approvalv2.OperatorEquals, RightFact: "resource.submittedBy"}}, Effect: approvalv2.EffectRequireEvidence, Requirements: []approvalv2.EvidenceRequirement{
			{ID: "seed.digest", Field: "review.expected-digest", Kind: approvalv2.EvidenceExactFactMatch, Fact: "resource.digest", Title: "确认冻结摘要"},
			{ID: "seed.acknowledged", Field: "review.acknowledged", Kind: approvalv2.EvidenceBooleanTrue, Title: "确认已复核"},
			{ID: "seed.reason", Field: "review.reason", Kind: approvalv2.EvidenceTextLength, MinLength: 4, MaxLength: 512, Title: "审批原因", Audit: true},
		}},
		{ID: "seed.other-actor", Priority: 50, Conditions: []approvalv2.Condition{{Left: "actor.id", Operator: approvalv2.OperatorNotEquals, RightFact: "resource.submittedBy"}}, Effect: approvalv2.EffectAllow},
	}}
}

func enterprisePolicy() approvalv2.PolicyProfile {
	return approvalv2.PolicyProfile{ID: "enterprise.portal-publication", Revision: 1, DefaultEffect: approvalv2.EffectDeny, Rules: []approvalv2.Rule{
		{ID: "enterprise.deny-self", Priority: 100, Conditions: []approvalv2.Condition{{Left: "actor.id", Operator: approvalv2.OperatorEquals, RightFact: "resource.submittedBy"}}, Effect: approvalv2.EffectDeny, Code: "approval.separation_required", Message: "当前策略不允许提交人审批自己的内容"},
		{ID: "enterprise.allow-other", Priority: 50, Conditions: []approvalv2.Condition{{Left: "actor.id", Operator: approvalv2.OperatorNotEquals, RightFact: "resource.submittedBy"}}, Effect: approvalv2.EffectAllow},
	}}
}

func input(actor, submittedBy string) approvalv2.EvaluationInput {
	return approvalv2.EvaluationInput{Operation: "portal.publication.approve", TenantID: "local", Actor: approvalv2.ActorFacts{ID: actor}, Resource: approvalv2.ResourceFacts{ID: "operations/1", Digest: strings.Repeat("a", 64), SubmittedBy: submittedBy}}
}

func evidence(digest, reason string) map[string]json.RawMessage {
	encodedDigest, _ := json.Marshal(digest)
	acknowledged, _ := json.Marshal(true)
	encodedReason, _ := json.Marshal(reason)
	return map[string]json.RawMessage{"review.expected-digest": encodedDigest, "review.acknowledged": acknowledged, "review.reason": encodedReason}
}
