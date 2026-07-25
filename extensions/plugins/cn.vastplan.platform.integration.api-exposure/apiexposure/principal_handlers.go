package apiexposure

import (
	"context"
	"fmt"
	"strings"
)

func (s *Service) handlePrincipalOperation(ctx context.Context, principal Principal, operation string, payload []byte) (any, error) {
	switch operation {
	case "list", "createDraft", "updateDraft", "submit", "approve", "publish", "retire":
		return s.handleExposureOperation(ctx, principal, operation, payload)
	case "listDataPlanes", "createDataPlaneDraft", "submitDataPlane", "approveDataPlane", "publishDataPlane", "retireDataPlane", "issueDataPlaneTicket":
		return s.handleDataPlaneOperation(ctx, principal, operation, payload)
	case "listAudit":
		return s.handleAuditOperation(ctx, principal, operation, payload)
	case "apiList", "apiCreateDraft", "apiUpdateDraft", "apiSubmit", "apiApprove", "apiPublish", "apiRetire":
		return s.handleGatewayOperation(ctx, principal, operation, payload)
	default:
		return nil, fmt.Errorf("不支持 API Exposure 用户操作 %q", operation)
	}
}

func (s *Service) handleExposureOperation(ctx context.Context, principal Principal, operation string, payload []byte) (any, error) {
	var result any
	switch operation {
	case "list":
		value, err := s.List(ctx, principal)
		if err != nil {
			return nil, err
		}
		result = map[string]any{"items": value}
	case "createDraft":
		var request CreateDraftRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.CreateDraft(ctx, principal, request)
		if err != nil {
			return nil, err
		}
		result = value
	case "updateDraft":
		var request UpdateDraftRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.UpdateDraft(ctx, principal, request)
		if err != nil {
			return nil, err
		}
		result = value
	case "submit", "approve", "publish":
		var request revisionRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.Transition(ctx, principal, request.RevisionID, operation)
		if err != nil {
			return nil, err
		}
		result = value
	case "retire":
		var request struct {
			ExposureID string `json:"exposureId"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		if err := s.Retire(ctx, principal, request.ExposureID); err != nil {
			return nil, err
		}
		result = map[string]bool{"retired": true}
	default:
		return nil, fmt.Errorf("不支持 API Exposure 用户操作 %q", operation)
	}
	return result, nil
}

func (s *Service) handleDataPlaneOperation(ctx context.Context, principal Principal, operation string, payload []byte) (any, error) {
	var result any
	switch operation {
	case "listDataPlanes":
		value, err := s.ListDataPlanes(ctx, principal)
		if err != nil {
			return nil, err
		}
		result = map[string]any{"items": value}
	case "createDataPlaneDraft":
		var request CreateDataPlaneDraftRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.CreateDataPlaneDraft(ctx, principal, request)
		if err != nil {
			return nil, err
		}
		result = value
	case "submitDataPlane", "approveDataPlane", "publishDataPlane":
		var request revisionRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		action := strings.TrimSuffix(operation, "DataPlane")
		value, err := s.TransitionDataPlane(ctx, principal, request.RevisionID, action)
		if err != nil {
			return nil, err
		}
		result = value
	case "retireDataPlane":
		var request struct {
			ExposureID string `json:"exposureId"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		if err := s.RetireDataPlane(ctx, principal, request.ExposureID); err != nil {
			return nil, err
		}
		result = map[string]bool{"retired": true}
	case "issueDataPlaneTicket":
		var request TicketRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.IssueTicket(ctx, principal, request)
		if err != nil {
			return nil, err
		}
		result = value
	default:
		return nil, fmt.Errorf("不支持 API Exposure 用户操作 %q", operation)
	}
	return result, nil
}

func (s *Service) handleAuditOperation(ctx context.Context, principal Principal, operation string, payload []byte) (any, error) {
	var result any
	switch operation {
	case "listAudit":
		if err := require(principal, "platform.api-exposure.read"); err != nil {
			return nil, err
		}
		s.mu.Lock()
		items := make([]AuditEvent, 0)
		for _, item := range s.state.Audit {
			if item.TenantID == principal.TenantID {
				items = append(items, item)
			}
		}
		s.mu.Unlock()
		result = map[string]any{"items": items}
	default:
		return nil, fmt.Errorf("不支持 API Exposure 用户操作 %q", operation)
	}
	return result, nil
}

func (s *Service) handleGatewayOperation(ctx context.Context, principal Principal, operation string, payload []byte) (any, error) {
	var result any
	switch operation {
	case "apiList":
		if _, err := gatewayInvocation(payload, "GET"); err != nil {
			return nil, err
		}
		value, err := s.List(ctx, principal)
		if err != nil {
			return nil, err
		}
		result = map[string]any{"items": value}
	case "apiCreateDraft":
		invocation, err := gatewayInvocation(payload, "POST")
		if err != nil {
			return nil, err
		}
		var request CreateDraftRequest
		if err := decode(invocation.Body, &request); err != nil {
			return nil, err
		}
		value, err := s.CreateDraft(ctx, principal, request)
		if err != nil {
			return nil, err
		}
		result = value
	case "apiUpdateDraft":
		invocation, err := gatewayInvocation(payload, "PUT")
		if err != nil {
			return nil, err
		}
		var request UpdateDraftRequest
		if err := decode(invocation.Body, &request); err != nil {
			return nil, err
		}
		request.RevisionID, err = pathUint64(invocation, "revisionId")
		if err != nil {
			return nil, err
		}
		value, err := s.UpdateDraft(ctx, principal, request)
		if err != nil {
			return nil, err
		}
		result = value
	case "apiSubmit", "apiApprove", "apiPublish":
		invocation, err := gatewayInvocation(payload, "POST")
		if err != nil {
			return nil, err
		}
		var request struct{}
		if err := decode(invocation.Body, &request); err != nil {
			return nil, err
		}
		revisionID, err := pathUint64(invocation, "revisionId")
		if err != nil {
			return nil, err
		}
		action := strings.TrimPrefix(operation, "api")
		action = strings.ToLower(action[:1]) + action[1:]
		value, err := s.Transition(ctx, principal, revisionID, action)
		if err != nil {
			return nil, err
		}
		result = value
	case "apiRetire":
		invocation, err := gatewayInvocation(payload, "POST")
		if err != nil {
			return nil, err
		}
		if err := s.Retire(ctx, principal, invocation.PathParams["exposureId"]); err != nil {
			return nil, err
		}
		result = map[string]bool{"retired": true}
	default:
		return nil, fmt.Errorf("不支持 API Exposure 用户操作 %q", operation)
	}
	return result, nil
}
