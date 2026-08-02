package portalcomposer

import (
	"errors"
	"fmt"

	approvalv1 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/approvalpolicy"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

type ApprovalError struct {
	Decision approvalv1.Decision
}

func (e *ApprovalError) Error() string { return e.Decision.Message }

func (e *ApprovalError) Unwrap() error {
	if e.Decision.Code == approvalpolicy.CodeSeparationRequired {
		return ErrSelfApproval
	}
	return nil
}

func (s *Service) approvalDecision(principal portalapi.Principal, revision portalapi.Revision, review approvalv1.ReviewEvidence) approvalv1.Decision {
	return s.approvalPolicy.Decide(approvalv1.Request{
		Operation: "portal.publication.approve", ResourceID: fmt.Sprintf("%s/%d", revision.PortalID, revision.ID),
		ResourceDigest: revision.ConfigurationDigest, ActorID: principal.ID, SubmittedBy: revision.SubmittedBy, Review: review,
	})
}

func (s *Service) requireApprovalLocked(principal portalapi.Principal, revision portalapi.Revision, request portalapi.PortalApprovalRequest) error {
	decision := s.approvalDecision(principal, revision, request.Review)
	if decision.Status == approvalv1.DecisionAllowed {
		return nil
	}
	return &ApprovalError{Decision: decision}
}

func approvalError(err error) (*ApprovalError, bool) {
	var target *ApprovalError
	ok := errors.As(err, &target)
	return target, ok
}
