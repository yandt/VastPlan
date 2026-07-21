package portaltrust

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

type catalogValidationRequest struct {
	TenantID string               `json:"tenantId"`
	Spec     portalapi.PortalSpec `json:"spec"`
}

type catalogTestArtifactRequest struct {
	TenantID          string                             `json:"tenantId"`
	Request           portalapi.CreateTestReleaseRequest `json:"request"`
	AllowedPublishers []string                           `json:"allowedPublishers"`
}

type PortalCatalog interface {
	ValidatePortal(context.Context, string, portalapi.PortalSpec) error
	MaterializePortal(context.Context, string, portalapi.PortalSpec) ([]pluginv1.ArtifactReference, error)
}

type PortalTestArtifactCatalog interface {
	ValidateTestArtifact(context.Context, string, portalapi.CreateTestReleaseRequest, []string) error
}

// CatalogValidationService exposes only complete Portal-spec validation to the
// Composer. Package bytes, repository credentials and verifier keys remain in
// the trusted Backend host.
func CatalogValidationService(catalog PortalCatalog) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		request, err := decodeCatalogRequest(catalog, callCtx, payload, "校验")
		if err != nil {
			return nil, nil, err
		}
		if err := catalog.ValidatePortal(ctx, request.TenantID, request.Spec); err != nil {
			return nil, nil, err
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"valid":true}`), nil
	}
}

// CatalogMaterializationService commits immutable browser/server delivery
// objects before a Portal Activation becomes publishable.
func CatalogMaterializationService(catalog PortalCatalog) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		request, err := decodeCatalogRequest(catalog, callCtx, payload, "物化")
		if err != nil {
			return nil, nil, err
		}
		references, err := catalog.MaterializePortal(ctx, request.TenantID, request.Spec)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(map[string]any{"materialized": true, "references": references})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, response, err
	}
}

func CatalogTestArtifactValidationService(catalog PortalTestArtifactCatalog) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if catalog == nil {
			return nil, nil, errors.New("可信 Portal 制品目录未配置")
		}
		if !authenticatedPluginCaller(callCtx) {
			return nil, nil, errors.New("Portal 测试制品验证只接受已认证插件会话")
		}
		var request catalogTestArtifactRequest
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || request.TenantID == "" || request.TenantID != callCtx.GetTenantId() {
			return nil, nil, errors.New("Portal 测试制品验证请求 tenant 无效")
		}
		if err := catalog.ValidateTestArtifact(ctx, request.TenantID, request.Request, request.AllowedPublishers); err != nil {
			return nil, nil, err
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"valid":true}`), nil
	}
}

func decodeCatalogRequest(catalog PortalCatalog, callCtx *contractv1.CallContext, payload []byte, operation string) (catalogValidationRequest, error) {
	if catalog == nil {
		return catalogValidationRequest{}, errors.New("可信 Portal 制品目录未配置")
	}
	if !authenticatedPluginCaller(callCtx) {
		return catalogValidationRequest{}, errors.New("Portal 制品" + operation + "只接受已认证插件会话")
	}
	var request catalogValidationRequest
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.TenantID == "" || request.TenantID != callCtx.GetTenantId() {
		return catalogValidationRequest{}, errors.New("Portal 制品" + operation + "请求 tenant 无效")
	}
	return request, nil
}

func authenticatedPluginCaller(callCtx *contractv1.CallContext) bool {
	return callCtx != nil && callCtx.GetCaller().GetKind() == contractv1.CallerKind_CALLER_KIND_PLUGIN && callCtx.GetCaller().GetId() != ""
}
