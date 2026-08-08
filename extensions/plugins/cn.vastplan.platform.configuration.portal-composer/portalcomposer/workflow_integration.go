package portalcomposer

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	workflowv1 "cdsoft.com.cn/VastPlan/contracts/schemas/workflow/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func (s *Service) PreparePortalPublication(ctx context.Context, principal portalapi.Principal, payload []byte) (workflowv1.PreparedResource, error) {
	var request struct {
		PortalID    string                                   `json:"portalId"`
		Publication portalapi.SubmitPortalPublicationRequest `json:"publication"`
	}
	if err := decode(payload, &request); err != nil {
		return workflowv1.PreparedResource{}, err
	}
	publication, err := s.SubmitPortalPublication(ctx, principal, request.PortalID, request.Publication)
	if err != nil {
		return workflowv1.PreparedResource{}, err
	}
	projection, err := json.Marshal(publication)
	if err != nil {
		return workflowv1.PreparedResource{}, err
	}
	return workflowv1.PreparedResource{Resource: workflowv1.ResourceRef{Kind: portalapi.WorkflowPublicationResourceKind, ID: strconv.FormatUint(publication.ID, 10)}, Digest: publication.Digest, Revision: int64(publication.WorkingRevision), Projection: projection}, nil
}

func (s *Service) ExecutePublicationRelease(ctx context.Context, principal portalapi.Principal, request workflowv1.ActionRequest) (portalapi.PortalRelease, error) {
	if !principal.System || principal.ID != workflowv1.OrchestratorPluginID || request.FeatureID != portalapi.WorkflowPublicationFeatureID || request.ActionID != portalapi.WorkflowPublicationReleaseActionID || request.Resource.Kind != portalapi.WorkflowPublicationResourceKind || len(request.ResourceDigest) != 64 || request.IdempotencyKey == "" {
		return portalapi.PortalRelease{}, ErrForbidden
	}
	publicationID, err := strconv.ParseUint(request.Resource.ID, 10, 64)
	if err != nil || publicationID == 0 {
		return portalapi.PortalRelease{}, ErrInvalidState
	}
	for {
		s.mu.Lock()
		index, findErr := s.revisionIndex(principal.TenantID, publicationID)
		if findErr != nil || s.isHiddenVersionLocked(publicationID) {
			s.mu.Unlock()
			return portalapi.PortalRelease{}, ErrNotFound
		}
		revision := s.state.Revisions[index]
		if revision.ConfigurationDigest != request.ResourceDigest {
			s.mu.Unlock()
			return portalapi.PortalRelease{}, fmt.Errorf("%w: Publication digest changed", ErrInvalidState)
		}
		for _, activation := range s.state.Activations {
			if activation.TenantID == principal.TenantID && activation.PortalID == revision.PortalID && activation.ApplicationRevisionID == publicationID {
				s.mu.Unlock()
				return projectRelease(activation), nil
			}
		}
		switch revision.Status {
		case portalapi.StatusPendingApproval:
			_, err = s.transitionPublicationLocked(ctx, principal, index, "approve", "portal.workflow.", request.InstanceID)
			s.mu.Unlock()
			if err != nil {
				return portalapi.PortalRelease{}, err
			}
		case portalapi.StatusApproved:
			_, err = s.transitionPublicationLocked(ctx, principal, index, "publish", "portal.workflow.", request.InstanceID)
			s.mu.Unlock()
			if err != nil {
				return portalapi.PortalRelease{}, err
			}
		case portalapi.StatusPublished:
			expectedCurrent := s.currentActivationIDLocked(principal.TenantID, revision.PortalID)
			s.mu.Unlock()
			return s.ReleasePortalVersion(ctx, principal, revision.PortalID, portalapi.PortalReleaseRequest{PortalVersionID: publicationID, ExpectedCurrentReleaseID: expectedCurrent, Reason: "workflow " + request.InstanceID})
		default:
			s.mu.Unlock()
			return portalapi.PortalRelease{}, ErrInvalidState
		}
	}
}
