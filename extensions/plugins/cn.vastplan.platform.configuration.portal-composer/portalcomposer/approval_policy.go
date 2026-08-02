package portalcomposer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

var ErrApprovalProviderUnavailable = errors.New("Approval Provider 暂不可用")

type ApprovalPolicy interface {
	Evaluate(context.Context, approvalv2.EvaluationInput) (approvalv2.Decision, error)
	EvaluateBatch(context.Context, []approvalv2.EvaluationInput) ([]approvalv2.Decision, error)
}

type approvalPolicyContextKey struct{}

func withApprovalPolicy(ctx context.Context, policy ApprovalPolicy) context.Context {
	return context.WithValue(ctx, approvalPolicyContextKey{}, policy)
}

func approvalPolicyFromContext(ctx context.Context) (ApprovalPolicy, error) {
	policy, _ := ctx.Value(approvalPolicyContextKey{}).(ApprovalPolicy)
	if policy == nil {
		return nil, ErrApprovalProviderUnavailable
	}
	return policy, nil
}

type ApprovalError struct {
	Decision approvalv2.Decision
}

func (e *ApprovalError) Error() string { return e.Decision.Message }

func (e *ApprovalError) Unwrap() error {
	if e.Decision.Code == "approval.separation_required" {
		return ErrSelfApproval
	}
	return nil
}

func (s *Service) approvalInput(principal portalapi.Principal, revision portalapi.Revision, review portalapi.PortalApprovalRequest) (approvalv2.EvaluationInput, error) {
	evidence := map[string]json.RawMessage{}
	if review.Review.ExpectedDigest != "" {
		evidence["review.expected-digest"], _ = json.Marshal(review.Review.ExpectedDigest)
	}
	if review.Review.Acknowledged {
		evidence["review.acknowledged"], _ = json.Marshal(true)
	}
	if review.Review.Reason != "" {
		evidence["review.reason"], _ = json.Marshal(review.Review.Reason)
	}
	input := approvalv2.EvaluationInput{
		Operation: "portal.publication.approve", TenantID: principal.TenantID,
		Actor: approvalv2.ActorFacts{ID: principal.ID, Roles: append([]string(nil), principal.Roles...)},
		Resource: approvalv2.ResourceFacts{
			ID: fmt.Sprintf("%s/%d", revision.PortalID, revision.ID), Digest: revision.ConfigurationDigest, SubmittedBy: revision.SubmittedBy,
		},
		Evidence: evidence,
	}
	if err := approvalv2.ValidateInput(input); err != nil {
		return approvalv2.EvaluationInput{}, err
	}
	return input, nil
}

func (s *Service) approvalDecision(ctx context.Context, principal portalapi.Principal, revision portalapi.Revision, review portalapi.PortalApprovalRequest) (approvalv2.Decision, error) {
	policy, err := approvalPolicyFromContext(ctx)
	if err != nil {
		return approvalv2.Decision{}, err
	}
	input, err := s.approvalInput(principal, revision, review)
	if err != nil {
		return approvalv2.Decision{}, err
	}
	decision, err := policy.Evaluate(ctx, input)
	if err != nil {
		return approvalv2.Decision{}, fmt.Errorf("%w: %v", ErrApprovalProviderUnavailable, err)
	}
	return decision, nil
}

func (s *Service) approvalDecisions(ctx context.Context, principal portalapi.Principal, revisions []portalapi.Revision) ([]approvalv2.Decision, error) {
	policy, err := approvalPolicyFromContext(ctx)
	if err != nil {
		return nil, err
	}
	inputs := make([]approvalv2.EvaluationInput, 0, len(revisions))
	for _, revision := range revisions {
		input, err := s.approvalInput(principal, revision, portalapi.PortalApprovalRequest{})
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	decisions, err := policy.EvaluateBatch(ctx, inputs)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrApprovalProviderUnavailable, err)
	}
	return decisions, nil
}

func requireAllowedApproval(decision approvalv2.Decision) error {
	if decision.Status == approvalv2.DecisionAllowed {
		return nil
	}
	return &ApprovalError{Decision: decision}
}

func approvalError(err error) (*ApprovalError, bool) {
	var target *ApprovalError
	ok := errors.As(err, &target)
	return target, ok
}
