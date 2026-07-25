package portalcomposer

import (
	"context"
	"fmt"

	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

func (s *Service) handlePortalOperation(ctx context.Context, principal portalapi.Principal, operation string, payload []byte) (any, error) {
	switch operation {
	case "createDraft", "updateDraft", "list", "governance":
		return s.handleCompositionOperation(ctx, principal, operation, payload)
	case "createProfileDraft", "updateProfileDraft", "transitionProfile":
		return s.handleProfileOperation(ctx, principal, operation, payload)
	case "createBindingDraft", "updateBindingDraft", "transitionBinding", "activate", "rollbackActivation", "listActivations":
		return s.handleBindingOperation(ctx, principal, operation, payload)
	case "listTestTargetBindings", "putTestTargetBinding", "listTestReleases", "createTestRelease", "rollbackTestRelease":
		return s.handleTestReleaseOperation(ctx, principal, operation, payload)
	case "submit", "approve", "publish", "audit":
		return s.handlePublicationOperation(ctx, principal, operation, payload)
	default:
		return nil, fmt.Errorf("不支持 Portal Composer 操作 %q", operation)
	}
}

func (s *Service) handleCompositionOperation(ctx context.Context, principal portalapi.Principal, operation string, payload []byte) (any, error) {
	var result any
	switch operation {
	case "createDraft":
		var composition frontendcompositionv1.ApplicationComposition
		if err := decode(payload, &composition); err != nil {
			return nil, err
		}
		value, err := s.CreateDraft(ctx, principal, composition)
		if err != nil {
			return nil, err
		}
		result = value
	case "updateDraft":
		var request struct {
			RevisionID  uint64                                       `json:"revisionId"`
			Composition frontendcompositionv1.ApplicationComposition `json:"composition"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		if request.RevisionID == 0 {
			return nil, fmt.Errorf("revisionId 必须大于 0")
		}
		value, err := s.UpdateDraft(ctx, principal, request.RevisionID, request.Composition)
		if err != nil {
			return nil, err
		}
		result = value
	case "list":
		value, err := s.List(ctx, principal)
		if err != nil {
			return nil, err
		}
		result = value
	case "governance":
		value, err := s.Governance(ctx, principal)
		if err != nil {
			return nil, err
		}
		result = value
	default:
		return nil, fmt.Errorf("不支持 Portal Composer 操作 %q", operation)
	}
	return result, nil
}

func (s *Service) handleProfileOperation(ctx context.Context, principal portalapi.Principal, operation string, payload []byte) (any, error) {
	var result any
	switch operation {
	case "createProfileDraft":
		var profile frontendcompositionv1.PlatformProfile
		if err := decode(payload, &profile); err != nil {
			return nil, err
		}
		value, err := s.CreateProfileDraft(ctx, principal, profile)
		if err != nil {
			return nil, err
		}
		result = value
	case "updateProfileDraft":
		var request struct {
			RevisionID uint64                                `json:"revisionId"`
			Profile    frontendcompositionv1.PlatformProfile `json:"profile"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.UpdateProfileDraft(ctx, principal, request.RevisionID, request.Profile)
		if err != nil {
			return nil, err
		}
		result = value
	case "transitionProfile":
		var request struct {
			RevisionID uint64 `json:"revisionId"`
			Action     string `json:"action"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.TransitionProfile(ctx, principal, request.RevisionID, request.Action)
		if err != nil {
			return nil, err
		}
		result = value
	default:
		return nil, fmt.Errorf("不支持 Portal Composer 操作 %q", operation)
	}
	return result, nil
}

func (s *Service) handleBindingOperation(ctx context.Context, principal portalapi.Principal, operation string, payload []byte) (any, error) {
	var result any
	switch operation {
	case "createBindingDraft":
		var request portalapi.BindingDraftRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.CreateBindingDraft(ctx, principal, request)
		if err != nil {
			return nil, err
		}
		result = value
	case "updateBindingDraft":
		var request struct {
			RevisionID uint64                        `json:"revisionId"`
			Draft      portalapi.BindingDraftRequest `json:"draft"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.UpdateBindingDraft(ctx, principal, request.RevisionID, request.Draft)
		if err != nil {
			return nil, err
		}
		result = value
	case "transitionBinding":
		var request struct {
			RevisionID uint64 `json:"revisionId"`
			Action     string `json:"action"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.TransitionBinding(ctx, principal, request.RevisionID, request.Action)
		if err != nil {
			return nil, err
		}
		result = value
	case "activate":
		var request portalapi.ActivationRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.Activate(ctx, principal, request)
		if err != nil {
			return nil, err
		}
		result = value
	case "rollbackActivation":
		var request struct {
			SourceID          uint64 `json:"sourceId"`
			ExpectedCurrentID uint64 `json:"expectedCurrentId"`
			Reason            string `json:"reason"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.RollbackActivation(ctx, principal, request.SourceID, request.ExpectedCurrentID, request.Reason)
		if err != nil {
			return nil, err
		}
		result = value
	case "listActivations":
		value, err := s.ListActivations(ctx, principal)
		if err != nil {
			return nil, err
		}
		result = value
	default:
		return nil, fmt.Errorf("不支持 Portal Composer 操作 %q", operation)
	}
	return result, nil
}

func (s *Service) handleTestReleaseOperation(ctx context.Context, principal portalapi.Principal, operation string, payload []byte) (any, error) {
	var result any
	switch operation {
	case "listTestTargetBindings":
		value, err := s.ListTestTargetBindings(ctx, principal)
		if err != nil {
			return nil, err
		}
		result = value
	case "putTestTargetBinding":
		var request struct {
			ID      string                                `json:"id"`
			Binding portalapi.PutTestTargetBindingRequest `json:"binding"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.PutTestTargetBinding(ctx, principal, request.ID, request.Binding)
		if err != nil {
			return nil, err
		}
		result = value
	case "listTestReleases":
		value, err := s.ListTestReleases(ctx, principal)
		if err != nil {
			return nil, err
		}
		result = value
	case "createTestRelease":
		var request portalapi.CreateTestReleaseRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.CreateTestRelease(ctx, principal, request)
		if err != nil {
			return nil, err
		}
		result = value
	case "rollbackTestRelease":
		var request struct {
			ID uint64 `json:"id"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.RollbackTestRelease(ctx, principal, request.ID)
		if err != nil {
			return nil, err
		}
		result = value
	default:
		return nil, fmt.Errorf("不支持 Portal Composer 操作 %q", operation)
	}
	return result, nil
}

func (s *Service) handlePublicationOperation(ctx context.Context, principal portalapi.Principal, operation string, payload []byte) (any, error) {
	var result any
	switch operation {
	case "submit", "approve", "publish", "audit":
		var request struct {
			RevisionID uint64 `json:"revisionId"`
			portalapi.PublishRequest
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		if request.RevisionID == 0 {
			return nil, fmt.Errorf("revisionId 必须大于 0")
		}
		switch operation {
		case "submit":
			value, err := s.Submit(ctx, principal, request.RevisionID)
			if err != nil {
				return nil, err
			}
			result = value
		case "approve":
			value, err := s.Approve(ctx, principal, request.RevisionID)
			if err != nil {
				return nil, err
			}
			result = value
		case "publish":
			value, err := s.Publish(ctx, principal, request.RevisionID, request.PublishRequest)
			if err != nil {
				return nil, err
			}
			result = value
		case "audit":
			value, err := s.Audit(ctx, principal, request.RevisionID)
			if err != nil {
				return nil, err
			}
			result = value
		}
	default:
		return nil, fmt.Errorf("不支持 Portal Composer 操作 %q", operation)
	}
	return result, nil
}
