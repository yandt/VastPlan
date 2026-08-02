// Package approvalv1 defines the framework-neutral approval policy contract.
// Authorization answers whether an actor may request an operation; this
// contract answers whether the business approval evidence is sufficient.
package approvalv1

import (
	"errors"
	"regexp"
	"strings"
)

const Protocol = "approval.policy.v1"

type Mode string

const (
	ModeDifferentSubject     Mode = "different-subject"
	ModeSingleOperatorReview Mode = "single-operator-review"
)

type Profile struct {
	Protocol                     string `json:"protocol"`
	ID                           string `json:"id"`
	Mode                         Mode   `json:"mode"`
	RequireReason                bool   `json:"requireReason"`
	RequireDigestAcknowledgement bool   `json:"requireDigestAcknowledgement"`
}

type ReviewEvidence struct {
	ExpectedDigest string `json:"expectedDigest,omitempty"`
	Acknowledged   bool   `json:"acknowledged,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type Request struct {
	Operation      string         `json:"operation"`
	ResourceID     string         `json:"resourceId"`
	ResourceDigest string         `json:"resourceDigest"`
	ActorID        string         `json:"actorId"`
	SubmittedBy    string         `json:"submittedBy"`
	Review         ReviewEvidence `json:"review"`
}

type DecisionStatus string

const (
	DecisionAllowed        DecisionStatus = "allowed"
	DecisionReviewRequired DecisionStatus = "review-required"
	DecisionDenied         DecisionStatus = "denied"
)

type Decision struct {
	Status    DecisionStatus `json:"status"`
	PolicyID  string         `json:"policyId"`
	Code      string         `json:"code,omitempty"`
	Message   string         `json:"message,omitempty"`
	AuditNote string         `json:"auditNote,omitempty"`
}

var governedID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$`)

func ValidateProfile(profile Profile) error {
	if profile.Protocol != Protocol || !governedID.MatchString(profile.ID) {
		return errors.New("Approval Policy protocol 或 ID 无效")
	}
	if profile.Mode != ModeDifferentSubject && profile.Mode != ModeSingleOperatorReview {
		return errors.New("Approval Policy mode 无效")
	}
	if profile.Mode == ModeSingleOperatorReview && (!profile.RequireReason || !profile.RequireDigestAcknowledgement) {
		return errors.New("单人复验策略必须要求原因和冻结摘要确认")
	}
	return nil
}

func NormalizeReview(review ReviewEvidence) ReviewEvidence {
	review.ExpectedDigest = strings.TrimSpace(review.ExpectedDigest)
	review.Reason = strings.TrimSpace(review.Reason)
	return review
}
