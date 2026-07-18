package portalcomposer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/errorcode"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type catalogContextKey struct{}

func withCatalog(ctx context.Context, catalog Catalog) context.Context {
	return context.WithValue(ctx, catalogContextKey{}, catalog)
}

func (s *Service) validateCatalog(ctx context.Context, tenantID string, spec portalapi.PortalSpec) error {
	catalog, _ := ctx.Value(catalogContextKey{}).(Catalog)
	if catalog == nil {
		catalog = s.catalog
	}
	if catalog == nil {
		return fmt.Errorf("Portal Composer 未获得受信任制品目录")
	}
	return catalog.ValidatePortal(ctx, tenantID, spec)
}

func (s *Service) materializeCatalog(ctx context.Context, tenantID string, spec portalapi.PortalSpec) error {
	catalog, _ := ctx.Value(catalogContextKey{}).(Catalog)
	if catalog == nil {
		catalog = s.catalog
	}
	if catalog == nil {
		return fmt.Errorf("Portal Composer 未获得受信任制品目录")
	}
	return catalog.MaterializePortal(ctx, tenantID, spec)
}

// hostCatalog makes the artifact decision in the trusted kernel boundary. The
// plugin can ask whether a spec is valid, but never receives repository
// credentials, verification keys, or an unverified artifact envelope.
type hostCatalog struct {
	host    sdk.Host
	callCtx *contractv1.CallContext
}

func (c hostCatalog) ValidatePortal(ctx context.Context, tenantID string, spec portalapi.PortalSpec) error {
	return c.call(ctx, tenantID, spec, portalapi.KernelCatalogValidationCapability, "validate")
}

func (c hostCatalog) MaterializePortal(ctx context.Context, tenantID string, spec portalapi.PortalSpec) error {
	return c.call(ctx, tenantID, spec, portalapi.KernelCatalogMaterializationCapability, "materialize")
}

func (c hostCatalog) call(ctx context.Context, tenantID string, spec portalapi.PortalSpec, capability, operation string) error {
	if c.host == nil || c.callCtx == nil || strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("Portal 制品目录调用上下文不完整")
	}
	payload, err := json.Marshal(struct {
		TenantID string               `json:"tenantId"`
		Spec     portalapi.PortalSpec `json:"spec"`
	}{TenantID: tenantID, Spec: spec})
	if err != nil {
		return err
	}
	op := operation
	result, _, err := c.host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.KernelService,
		Capability:     capability,
		Operation:      &op,
	}, c.callCtx, payload)
	if err != nil {
		return fmt.Errorf("调用可信 Portal 制品目录 %s: %w", operation, err)
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return fmt.Errorf("可信 Portal 制品目录拒绝校验")
	}
	return nil
}

func (s *Service) ensureConfigured(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext) error {
	s.mu.Lock()
	configured := s.stateFile != "" && s.profileConfigured
	s.mu.Unlock()
	if configured {
		return nil
	}
	raw, err := readConfig(ctx, host, callCtx, StateFileConfigKey)
	if err != nil {
		return err
	}
	var stateFile string
	if err := json.Unmarshal(raw, &stateFile); err != nil || strings.TrimSpace(stateFile) == "" {
		return fmt.Errorf("%s 必须是非空 JSON 字符串", StateFileConfigKey)
	}
	profileRaw, err := readConfig(ctx, host, callCtx, PlatformProfileConfigKey)
	if err != nil {
		return err
	}
	var encodedProfile string
	if err := json.Unmarshal(profileRaw, &encodedProfile); err != nil || strings.TrimSpace(encodedProfile) == "" {
		return fmt.Errorf("%s 必须是非空 JSON 字符串", PlatformProfileConfigKey)
	}
	profile, err := frontendcompositionv1.ParsePlatformProfile([]byte(encodedProfile))
	if err != nil {
		return err
	}
	s.mu.Lock()
	err = s.configure(stateFile)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.BindPlatformProfile(profile)
}

func readConfig(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, key string) ([]byte, error) {
	op := "get"
	payload, _ := json.Marshal(map[string]string{"key": key})
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.KernelService, Capability: "kernel.config.get", Operation: &op}, callCtx, payload)
	if err != nil {
		return nil, fmt.Errorf("读取 Portal Composer 部署配置 %s: %w", key, err)
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return nil, fmt.Errorf("未提供 Portal Composer 部署配置 %s", key)
	}
	return raw, nil
}

// Handle is the wire boundary used by the plugin capability adapter. Principal
// is supplied by the trusted host call context, never decoded from browser JSON.
func (s *Service) Handle(ctx context.Context, principal portalapi.Principal, operation string, payload []byte) ([]byte, error) {
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
	case "submit", "approve", "publish", "rollback", "audit":
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
		case "rollback":
			value, err := s.Rollback(ctx, principal, request.RevisionID, request.PublishRequest)
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
	return json.Marshal(result)
}
func decode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("Portal Composer 请求无效: %w", err)
	}
	return nil
}

// Contribution exposes governance through the standard capability bus. The
// Host has already authenticated the caller; this adapter only projects the
// minimum fields required by the portal API.
func Contribution(service *Service) sdk.Contribution {
	handlers := map[string]sdk.Handler{}
	for _, operation := range []string{"createDraft", "updateDraft", "list", "submit", "approve", "publish", "rollback", "audit"} {
		op := operation
		handlers[op] = func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			if err := service.ensureConfigured(ctx, host, callCtx); err != nil {
				return nil, nil, err
			}
			principal, err := projectPrincipal(callCtx)
			if err != nil {
				return nil, nil, err
			}
			raw, err := service.Handle(withCatalog(ctx, hostCatalog{host: host, callCtx: callCtx}), principal, op, payload)
			if err != nil {
				if errors.Is(err, ErrForbidden) || errors.Is(err, ErrSelfApproval) {
					return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: errorcode.PermissionDenied, Message: err.Error()}}, nil, nil
				}
				return nil, nil, err
			}
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
		}
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: Descriptor(), Handlers: handlers}
}

func Descriptor() []byte {
	return []byte(`{"title":"门户组合治理","subcommands":[{"name":"createDraft","description":"创建 Portal 草稿"},{"name":"updateDraft","description":"更新 Portal 草稿"},{"name":"list","description":"列出 Portal revisions"},{"name":"submit","description":"提交草稿审批"},{"name":"approve","description":"审批草稿"},{"name":"publish","description":"发布 Portal revision"},{"name":"rollback","description":"回滚到历史 revision"},{"name":"audit","description":"读取 revision 审计"}]}`)
}
func projectPrincipal(callCtx *contractv1.CallContext) (portalapi.Principal, error) {
	if callCtx == nil || callCtx.Principal == nil || callCtx.Principal.UserId == "" || callCtx.TenantId == "" {
		return portalapi.Principal{}, fmt.Errorf("Portal capability 必须携带经验证的 Principal 和 tenant")
	}
	roles := append([]string(nil), callCtx.Principal.SystemRoles...)
	if callCtx.Principal.IsAdmin {
		roles = append(roles, "portal.compose", "portal.approve", "portal.publish")
	}
	return portalapi.Principal{ID: callCtx.Principal.UserId, TenantID: callCtx.TenantId, Roles: roles, System: callCtx.Principal.UserId == "system"}, nil
}
