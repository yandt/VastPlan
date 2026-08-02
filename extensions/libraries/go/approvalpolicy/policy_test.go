package approvalpolicy

import (
	"strings"
	"testing"

	approvalv1 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v1"
)

func TestDifferentSubjectPolicySeparatesAuthorizationFromApproval(t *testing.T) {
	policy := MustDifferentSubject()
	request := approvalv1.Request{ActorID: "alice", SubmittedBy: "alice", ResourceDigest: strings.Repeat("a", 64)}
	if decision := policy.Decide(request); decision.Status != approvalv1.DecisionDenied || decision.Code != CodeSeparationRequired {
		t.Fatalf("同一主体必须由审批策略拒绝: %+v", decision)
	}
	request.ActorID = "bob"
	if decision := policy.Decide(request); decision.Status != approvalv1.DecisionAllowed {
		t.Fatalf("不同主体应通过审批策略: %+v", decision)
	}
}

func TestSingleOperatorReviewBindsFrozenDigestAndReason(t *testing.T) {
	policy, err := New(approvalv1.Profile{
		Protocol: approvalv1.Protocol, ID: "foundation.approval.seed-review", Mode: approvalv1.ModeSingleOperatorReview,
		RequireReason: true, RequireDigestAcknowledgement: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("b", 64)
	request := approvalv1.Request{ActorID: "alice", SubmittedBy: "alice", ResourceDigest: digest}
	if decision := policy.Decide(request); decision.Status != approvalv1.DecisionReviewRequired {
		t.Fatalf("缺少复验证据必须返回 review-required: %+v", decision)
	}
	request.Review = approvalv1.ReviewEvidence{ExpectedDigest: strings.Repeat("c", 64), Acknowledged: true, Reason: "已检查配置"}
	if decision := policy.Decide(request); decision.Code != CodeDigestMismatch {
		t.Fatalf("摘要漂移必须拒绝: %+v", decision)
	}
	request.Review.ExpectedDigest = digest
	if decision := policy.Decide(request); decision.Status != approvalv1.DecisionAllowed || !strings.Contains(decision.AuditNote, digest) {
		t.Fatalf("有效复验应通过并生成审计说明: %+v", decision)
	}
}
