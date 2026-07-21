package edge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	"cdsoft.com.cn/VastPlan/core/shared/go/callcontext"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/interactionapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

// ProtocolBusCapabilityClient adapts Edge's narrow capability port to the
// backend host. The target remains a neutral portalapi constant, so Edge never
// imports a concrete plugin implementation.
type ProtocolBusCapabilityClient struct{ host *protocolbus.Host }

func NewProtocolBusCapabilityClient(host *protocolbus.Host) (*ProtocolBusCapabilityClient, error) {
	if host == nil {
		return nil, errors.New("Portal capability protocol host 不能为空")
	}
	return &ProtocolBusCapabilityClient{host: host}, nil
}

func (c *ProtocolBusCapabilityClient) Call(ctx context.Context, p portalapi.Principal, operation string, payload []byte) ([]byte, error) {
	return invokeCapability(ctx, c.host, p, portalapi.ComposerCapability, operation, payload)
}

// ProtocolBusInteractionClient uses the same authenticated Edge-to-host bridge
// as Portal governance, but targets the independent interaction capability.
// Keeping the target here prevents HTTP handlers from knowing plugin transport.
type ProtocolBusInteractionClient struct{ host *protocolbus.Host }

func NewProtocolBusInteractionClient(host *protocolbus.Host) (*ProtocolBusInteractionClient, error) {
	if host == nil {
		return nil, errors.New("Interaction capability protocol host 不能为空")
	}
	return &ProtocolBusInteractionClient{host: host}, nil
}

func (c *ProtocolBusInteractionClient) Call(ctx context.Context, p portalapi.Principal, operation string, payload []byte) ([]byte, error) {
	return invokeCapability(ctx, c.host, p, interactionapi.Capability, operation, payload)
}

// AddressingPlatformCapabilityClient is a deliberately allowlisted bridge to
// shared platform services. It does not expose a generic capability proxy.
type AddressingPlatformCapabilityClient struct{ router *addressing.Router }

func NewAddressingPlatformCapabilityClient(router *addressing.Router) (*AddressingPlatformCapabilityClient, error) {
	if router == nil {
		return nil, errors.New("平台管理 addressing router 不能为空")
	}
	return &AddressingPlatformCapabilityClient{router: router}, nil
}

func (c *AddressingPlatformCapabilityClient) Call(ctx context.Context, p portalapi.Principal, management portalapi.ManagementTarget, capability, operation string, payload []byte) ([]byte, error) {
	if !platformCapabilityAllowed(capability, operation) || !management.AllowsOperation(capability, operation) {
		return nil, errors.New("平台管理能力或操作不在 Edge 白名单")
	}
	trusted, err := trustedPortalCallContext(p)
	if err != nil {
		return nil, err
	}
	logicalService, routingDomain := management.Service.LogicalService, management.Service.RoutingDomain
	target := &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: capability, Operation: &operation, LogicalService: &logicalService, RoutingDomain: &routingDomain}
	result, response, err := c.router.Invoke(callcontext.WithTrusted(ctx, trusted), target, trusted.Wire(), payload)
	if err != nil {
		return nil, fmt.Errorf("调用远端 capability %s: %w", capability, err)
	}
	if result == nil {
		return nil, fmt.Errorf("capability %s 响应为空", capability)
	}
	if result.Status != contractv1.CallResult_STATUS_OK {
		if result.Error != nil {
			return nil, &CapabilityError{Code: result.Error.Code, Message: result.Error.Message}
		}
		return nil, &CapabilityError{Message: fmt.Sprintf("capability %s 未成功", capability)}
	}
	return append([]byte(nil), response...), nil
}

func platformCapabilityAllowed(capability, operation string) bool {
	operations := map[string]map[string]struct{}{
		platformadminapi.SettingsCapability:    {"list": {}, "put": {}, "delete": {}},
		platformadminapi.CredentialsCapability: {"list": {}, "put": {}, "rotate": {}, "revoke": {}},
		platformadminapi.DatabaseCapability:    {"list": {}, "define": {}, "remove": {}, "probe": {}},
		platformadminapi.ArtifactsCapability:   {"status": {}, "listCatalog": {}, "listPublishJournal": {}, "resolve": {}, "setLifecycle": {}, "migrationStatus": {}, "prepareMigration": {}, "syncMigration": {}, "cutoverMigration": {}, "rollbackMigration": {}, "finalizeMigration": {}, "releaseMigration": {}},
		platformadminapi.DeploymentCapability:  {"listNodes": {}, "putNode": {}, "listBootstrapJobs": {}, "createBootstrap": {}, "approveBootstrap": {}, "listDeploymentTargets": {}, "listServiceRevisions": {}, "createServiceDraft": {}, "updateServiceDraft": {}, "submitServiceDraft": {}, "approveServiceRevision": {}, "publishServiceRevision": {}, "rollbackServiceRevision": {}, "listServiceRevisionAudit": {}, "listTestTargetBindings": {}, "putTestTargetBinding": {}, "listTestReleases": {}, "createTestRelease": {}, "rollbackTestRelease": {}},
	}
	_, ok := operations[capability][operation]
	return ok
}

func invokeCapability(ctx context.Context, host *protocolbus.Host, p portalapi.Principal, capability, operation string, payload []byte) ([]byte, error) {
	if p.ID == "" || p.TenantID == "" || operation == "" {
		return nil, errors.New("能力调用身份或操作不能为空")
	}
	trusted, err := trustedPortalCallContext(p)
	if err != nil {
		return nil, err
	}
	ctx = callcontext.WithTrusted(ctx, trusted)
	response, err := host.Invoke(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage,
		Capability:     capability,
		Operation:      &operation,
	}, trusted.Wire(), payload)
	if err != nil {
		return nil, fmt.Errorf("调用 capability %s: %w", capability, err)
	}
	if response == nil || response.Result == nil {
		return nil, fmt.Errorf("capability %s 响应为空", capability)
	}
	if response.Result.Status != contractv1.CallResult_STATUS_OK {
		if response.Result.Error != nil {
			return nil, &CapabilityError{Code: response.Result.Error.Code, Message: response.Result.Error.Message}
		}
		return nil, &CapabilityError{Message: fmt.Sprintf("capability %s 未成功", capability)}
	}
	return append([]byte(nil), response.Payload...), nil
}

func trustedPortalCallContext(p portalapi.Principal) (callcontext.Trusted, error) {
	if p.ID == "" || p.TenantID == "" {
		return callcontext.Trusted{}, errors.New("能力调用身份不能为空")
	}
	callerKind := contractv1.CallerKind_CALLER_KIND_USER
	if p.System {
		callerKind = contractv1.CallerKind_CALLER_KIND_SYSTEM
	}
	wire := &contractv1.CallContext{
		Caller:    &contractv1.Caller{Kind: callerKind, Id: p.ID},
		Principal: &contractv1.Principal{UserId: p.ID, TenantId: p.TenantID, SystemRoles: append([]string(nil), p.Roles...), IsAdmin: hasRole(p.Roles, "platform.admin")},
		TenantId:  p.TenantID,
		Scene:     "portal.bff",
	}
	trusted, err := callcontext.ValidateIngress(wire, callcontext.Provenance{Source: "portal.edge", AuthenticatedBy: "edge.identity"})
	if err != nil {
		return callcontext.Trusted{}, fmt.Errorf("构造可信 Portal 调用上下文: %w", err)
	}
	return trusted, nil
}

type catalogValidationRequest struct {
	TenantID string               `json:"tenantId"`
	Spec     portalapi.PortalSpec `json:"spec"`
}

type PortalCatalog interface {
	ValidatePortal(context.Context, string, portalapi.PortalSpec) error
	MaterializePortal(context.Context, string, portalapi.PortalSpec) error
}

type PortalTestArtifactCatalog interface {
	ValidateTestArtifact(context.Context, string, portalapi.CreateTestReleaseRequest, []string) error
}

type catalogTestArtifactRequest struct {
	TenantID          string                             `json:"tenantId"`
	Request           portalapi.CreateTestReleaseRequest `json:"request"`
	AllowedPublishers []string                           `json:"allowedPublishers"`
}

func CatalogTestArtifactValidationService(catalog PortalTestArtifactCatalog) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if catalog == nil {
			return nil, nil, errors.New("可信 Portal 制品目录未配置")
		}
		if callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() == "" {
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

// CatalogMaterializationService performs the same authenticated tenant
// projection but commits immutable browser delivery objects before publish.
func CatalogMaterializationService(catalog PortalCatalog) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if catalog == nil {
			return nil, nil, errors.New("可信 Portal 制品目录未配置")
		}
		if callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() == "" {
			return nil, nil, errors.New("Portal 制品物化只接受已认证插件会话")
		}
		var request catalogValidationRequest
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || request.TenantID == "" || request.TenantID != callCtx.GetTenantId() {
			return nil, nil, errors.New("Portal 制品物化请求 tenant 无效")
		}
		if err := catalog.MaterializePortal(ctx, request.TenantID, request.Spec); err != nil {
			return nil, nil, err
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"materialized":true}`), nil
	}
}

// CatalogValidationService exposes just one kernel service to the Composer:
// validate a complete Portal spec. The signed plugin cannot obtain artifact
// contents, repository credentials, or verifier keys through this endpoint.
func CatalogValidationService(catalog PortalCatalog) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if catalog == nil {
			return nil, nil, errors.New("可信 Portal 制品目录未配置")
		}
		if callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() == "" {
			return nil, nil, errors.New("Portal 制品校验只接受已认证插件会话")
		}
		var request catalogValidationRequest
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || request.TenantID == "" || request.TenantID != callCtx.GetTenantId() {
			return nil, nil, errors.New("Portal 制品校验请求 tenant 无效")
		}
		if err := catalog.ValidatePortal(ctx, request.TenantID, request.Spec); err != nil {
			return nil, nil, err
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"valid":true}`), nil
	}
}
