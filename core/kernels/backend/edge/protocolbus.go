package edge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"cdsoft.com.cn/VastPlan/core/shared/go/callcontext"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/interactionapi"
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

func invokeCapability(ctx context.Context, host *protocolbus.Host, p portalapi.Principal, capability, operation string, payload []byte) ([]byte, error) {
	if p.ID == "" || p.TenantID == "" || operation == "" {
		return nil, errors.New("能力调用身份或操作不能为空")
	}
	callerKind := contractv1.CallerKind_CALLER_KIND_USER
	if p.System {
		callerKind = contractv1.CallerKind_CALLER_KIND_SYSTEM
	}
	op := operation
	wire := &contractv1.CallContext{
		Caller:    &contractv1.Caller{Kind: callerKind, Id: p.ID},
		Principal: &contractv1.Principal{UserId: p.ID, TenantId: p.TenantID, SystemRoles: append([]string(nil), p.Roles...)},
		TenantId:  p.TenantID,
		Scene:     "portal.bff",
	}
	trusted, err := callcontext.ValidateIngress(wire, callcontext.Provenance{Source: "portal.edge", AuthenticatedBy: "edge.identity"})
	if err != nil {
		return nil, fmt.Errorf("构造可信 Portal 调用上下文: %w", err)
	}
	ctx = callcontext.WithTrusted(ctx, trusted)
	response, err := host.Invoke(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage,
		Capability:     capability,
		Operation:      &op,
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

type catalogValidationRequest struct {
	TenantID string               `json:"tenantId"`
	Spec     portalapi.PortalSpec `json:"spec"`
}

type PortalCatalog interface {
	ValidatePortal(context.Context, string, portalapi.PortalSpec) error
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
