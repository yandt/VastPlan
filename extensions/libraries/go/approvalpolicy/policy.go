// Package approvalpolicy provides the built-in adapters for approval.policy.v1.
// Domain plugins depend on Policy and do not branch on deployment environment.
package approvalpolicy

import (
	"fmt"
	"regexp"
	"strings"

	approvalv1 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v1"
)

const (
	CodeSeparationRequired = "approval.separation_required"
	CodeReviewRequired     = "approval.review_required"
	CodeDigestMismatch     = "approval.digest_mismatch"
	CodeReasonRequired     = "approval.reason_required"
)

type Policy interface {
	Profile() approvalv1.Profile
	Decide(approvalv1.Request) approvalv1.Decision
}

func New(profile approvalv1.Profile) (Policy, error) {
	if err := approvalv1.ValidateProfile(profile); err != nil {
		return nil, err
	}
	return fixedPolicy{profile: profile}, nil
}

func MustDifferentSubject() Policy {
	policy, err := New(approvalv1.Profile{
		Protocol: approvalv1.Protocol, ID: "foundation.approval.different-subject", Mode: approvalv1.ModeDifferentSubject,
	})
	if err != nil {
		panic(err)
	}
	return policy
}

type fixedPolicy struct{ profile approvalv1.Profile }

func (p fixedPolicy) Profile() approvalv1.Profile { return p.profile }

func (p fixedPolicy) Decide(request approvalv1.Request) approvalv1.Decision {
	if request.ActorID != request.SubmittedBy {
		return p.decision(approvalv1.DecisionAllowed, "", "", "different-subject")
	}
	if p.profile.Mode == approvalv1.ModeDifferentSubject {
		return p.decision(approvalv1.DecisionDenied, CodeSeparationRequired, "提交人不能审批自己提交的内容", "")
	}
	review := approvalv1.NormalizeReview(request.Review)
	if review.ExpectedDigest == "" && !review.Acknowledged && review.Reason == "" {
		return p.decision(approvalv1.DecisionReviewRequired, CodeReviewRequired, "当前策略要求单人复验冻结内容", "")
	}
	if p.profile.RequireDigestAcknowledgement && (!review.Acknowledged || !digest.MatchString(review.ExpectedDigest) || review.ExpectedDigest != request.ResourceDigest) {
		return p.decision(approvalv1.DecisionDenied, CodeDigestMismatch, "冻结内容摘要确认不一致，请刷新后重新复验", "")
	}
	if p.profile.RequireReason && (len(review.Reason) < 4 || len(review.Reason) > 512) {
		return p.decision(approvalv1.DecisionDenied, CodeReasonRequired, "单人复验必须填写 4 至 512 个字符的审批原因", "")
	}
	note := fmt.Sprintf("single-operator-review digest=%s reason=%s", request.ResourceDigest, strings.ReplaceAll(review.Reason, "\n", " "))
	return p.decision(approvalv1.DecisionAllowed, "", "", note)
}

func (p fixedPolicy) decision(status approvalv1.DecisionStatus, code, message, audit string) approvalv1.Decision {
	return approvalv1.Decision{Status: status, PolicyID: p.profile.ID, Code: code, Message: message, AuditNote: audit}
}

var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)
