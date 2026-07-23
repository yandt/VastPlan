package pluginsettings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	configurationresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationresource/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type operationRequest struct {
	ID                   string            `json:"id"`
	ConfigurationID      string            `json:"configurationId"`
	ResourceCollectionID string            `json:"resourceCollectionId"`
	ResourceID           string            `json:"resourceId"`
	CatalogDigest        string            `json:"catalogDigest"`
	ScopeSubjectID       string            `json:"scopeSubjectId"`
	Cursor               string            `json:"cursor"`
	Limit                uint32            `json:"limit"`
	Values               json.RawMessage   `json:"values"`
	Secrets              map[string]string `json:"secrets"`
	ExpectedRevision     uint64            `json:"expectedRevision"`
}

func ensureEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ErrInvalid
	}
	return nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return ensureEOF(decoder)
}

func (s *Service) Handler(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte, operation string) (*contractv1.CallResult, []byte, error) {
	if err := s.ensureConfigured(ctx, host, call); err != nil {
		return domainError("platform.plugin_configuration.unavailable", err)
	}
	s.workflowMu.Lock()
	defer s.workflowMu.Unlock()
	if err := s.recoverInterrupted(ctx, host, call); err != nil {
		return domainError("platform.plugin_configuration.unavailable", err)
	}
	var request operationRequest
	if err := decodeStrict(payload, &request); err != nil {
		return domainError("platform.plugin_configuration.invalid", ErrInvalid)
	}
	out, err := s.execute(ctx, host, call, operation, request)
	if err != nil {
		return domainError(domainErrorCode(err), err)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func (s *Service) execute(ctx context.Context, host sdk.Host, call *contractv1.CallContext, operation string, request operationRequest) (any, error) {
	tenant, _, tenantErr := tenantAndActor(call)
	if tenantErr != nil {
		return nil, tenantErr
	}
	switch operation {
	case "listDefinitions":
		catalogs, err := s.catalogs(ctx, host, call)
		return map[string]any{"items": s.publicDefinitions(tenant, catalogs)}, err
	case "getDefinition":
		catalogs, err := s.catalogs(ctx, host, call)
		if err != nil {
			return nil, err
		}
		view, err := findDefinition(catalogs, request.ConfigurationID, request.CatalogDigest)
		if err != nil {
			return nil, err
		}
		return s.publicDefinition(tenant, view, request.ScopeSubjectID), nil
	case "listCandidates":
		items, err := s.ListCandidates(call)
		return map[string]any{"items": items}, err
	case "listResourceItems":
		return s.ListResourceItems(ctx, host, call, request.ConfigurationID, request.ResourceCollectionID, request.CatalogDigest, request.Cursor, request.Limit)
	case "getResourceItem":
		return s.GetResourceItem(ctx, host, call, request.ConfigurationID, request.ResourceCollectionID, request.ResourceID, request.CatalogDigest)
	case "createResourceDraft":
		return s.CreateResourceDraft(ctx, host, call, resourceDraftRequest{
			ConfigurationID: request.ConfigurationID, ResourceCollectionID: request.ResourceCollectionID, CatalogDigest: request.CatalogDigest,
			Action: configurationresourcev1.ActionCreate, Values: request.Values, Secrets: request.Secrets,
		})
	case "updateResourceDraft":
		return s.CreateResourceDraft(ctx, host, call, resourceDraftRequest{
			ConfigurationID: request.ConfigurationID, ResourceCollectionID: request.ResourceCollectionID, ResourceID: request.ResourceID, CatalogDigest: request.CatalogDigest,
			Action: configurationresourcev1.ActionUpdate, Values: request.Values, Secrets: request.Secrets,
		})
	case "deleteResourceDraft":
		return s.CreateResourceDraft(ctx, host, call, resourceDraftRequest{
			ConfigurationID: request.ConfigurationID, ResourceCollectionID: request.ResourceCollectionID, ResourceID: request.ResourceID, CatalogDigest: request.CatalogDigest,
			Action: configurationresourcev1.ActionDelete,
		})
	case "createDraft":
		return s.CreateDraft(ctx, host, call, pluginconfiguration.CreateDraftRequest{
			ConfigurationID: request.ConfigurationID,
			CatalogDigest:   request.CatalogDigest,
			ScopeSubjectID:  request.ScopeSubjectID,
			Values:          request.Values,
			Secrets:         request.Secrets,
		})
	case "discardDraft":
		return s.DiscardDraft(ctx, host, call, request.ID, request.ExpectedRevision)
	case "submitDraft":
		return s.SubmitDraft(ctx, host, call, request.ID, request.ExpectedRevision)
	case "activateCandidate":
		return s.ActivateCandidate(ctx, host, call, request.ID, request.ExpectedRevision)
	case "submitProfileDraft":
		return s.SubmitProfileDraft(ctx, host, call, request.ID, request.ExpectedRevision)
	case "approveProfileCandidate":
		return s.ApproveProfileCandidate(ctx, host, call, request.ID, request.ExpectedRevision)
	case "activateProfileCandidate":
		return s.ActivateProfileCandidate(ctx, host, call, request.ID, request.ExpectedRevision)
	case "abortProfileCandidate":
		return s.AbortProfileCandidate(ctx, host, call, request.ID, request.ExpectedRevision)
	case "submitHotServiceDraft":
		return s.SubmitHotServiceDraft(ctx, host, call, request.ID, request.ExpectedRevision)
	case "approveHotServiceCandidate":
		return s.ApproveHotServiceCandidate(call, request.ID, request.ExpectedRevision)
	case "activateHotServiceCandidate":
		return s.ActivateHotServiceCandidate(ctx, host, call, request.ID, request.ExpectedRevision)
	case "abortHotServiceCandidate":
		return s.AbortHotServiceCandidate(ctx, host, call, request.ID, request.ExpectedRevision)
	case "submitScopedDraft":
		return s.SubmitScopedDraft(ctx, host, call, request.ID, request.ExpectedRevision)
	case "approveScopedCandidate":
		return s.ApproveScopedCandidate(call, request.ID, request.ExpectedRevision)
	case "activateScopedCandidate":
		return s.ActivateScopedCandidate(ctx, host, call, request.ID, request.ExpectedRevision)
	case "abortScopedCandidate":
		return s.AbortScopedCandidate(call, request.ID, request.ExpectedRevision)
	case "submitResourceDraft":
		return s.SubmitResourceDraft(ctx, host, call, request.ID, request.ExpectedRevision)
	case "approveResourceCandidate":
		return s.ApproveResourceCandidate(call, request.ID, request.ExpectedRevision)
	case "activateResourceCandidate":
		return s.ActivateResourceCandidate(ctx, host, call, request.ID, request.ExpectedRevision)
	case "abortResourceCandidate":
		return s.AbortResourceCandidate(ctx, host, call, request.ID, request.ExpectedRevision)
	default:
		return nil, ErrInvalid
	}
}

func domainErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrNotFound):
		return "platform.plugin_configuration.not_found"
	case errors.Is(err, ErrConflict):
		return "platform.plugin_configuration.conflict"
	case strings.Contains(err.Error(), "不可用"):
		return "platform.plugin_configuration.unavailable"
	default:
		return "platform.plugin_configuration.invalid"
	}
}

func domainError(code string, err error) (*contractv1.CallResult, []byte, error) {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error()}}, nil, nil
}
