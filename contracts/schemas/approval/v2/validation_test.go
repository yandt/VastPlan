package approvalv2

import (
	"strings"
	"testing"
)

func TestProfileDigestIsOrderIndependentForRules(t *testing.T) {
	first := seedProfile()
	second := seedProfile()
	second.Rules[0], second.Rules[1] = second.Rules[1], second.Rules[0]
	a, err := ProfileDigest(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ProfileDigest(second)
	if err != nil || a != b {
		t.Fatalf("规则顺序不得改变 Profile digest: a=%s b=%s err=%v", a, b, err)
	}
}

func TestProfileRejectsUnsafeDefaultAndInvalidCondition(t *testing.T) {
	profile := seedProfile()
	profile.DefaultEffect = EffectAllow
	if err := ValidateProfile(profile); err == nil {
		t.Fatal("Native Profile 不得使用 allow 默认效果")
	}
	profile = seedProfile()
	profile.Rules[0].Conditions[0].Left = "browser.claimedRole"
	if err := ValidateProfile(profile); err == nil {
		t.Fatal("未知事实引用必须拒绝")
	}
}

func TestReviewDecisionRequiresGovernedEvidence(t *testing.T) {
	decision := Decision{Status: DecisionReviewRequired, Profile: ProfileRef{ID: "seed.portal-publication", Revision: 1, Digest: strings.Repeat("a", 64)}}
	if err := ValidateDecision(decision); err == nil {
		t.Fatal("review-required 不能缺少可执行的证据要求")
	}
	decision.Requirements = []EvidenceRequirement{{ID: "seed.digest", Field: "review.expected-digest", Kind: EvidenceExactFactMatch, Fact: "resource.digest"}}
	if err := ValidateDecision(decision); err != nil {
		t.Fatal(err)
	}
}

func seedProfile() PolicyProfile {
	return PolicyProfile{ID: "seed.portal-publication", Revision: 1, DefaultEffect: EffectDeny, Rules: []Rule{
		{ID: "seed.self-review", Priority: 100, Conditions: []Condition{{Left: "actor.id", Operator: OperatorEquals, RightFact: "resource.submittedBy"}}, Effect: EffectRequireEvidence, Requirements: []EvidenceRequirement{{ID: "seed.digest", Field: "review.expected-digest", Kind: EvidenceExactFactMatch, Fact: "resource.digest"}}},
		{ID: "seed.other-actor", Priority: 50, Conditions: []Condition{{Left: "actor.id", Operator: OperatorNotEquals, RightFact: "resource.submittedBy"}}, Effect: EffectAllow},
	}}
}
