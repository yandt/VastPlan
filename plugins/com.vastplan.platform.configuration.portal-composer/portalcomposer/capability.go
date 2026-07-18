package portalcomposer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	sdk "cdsoft.com.cn/VastPlan/sdk/go/plugin"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/shared/go/portalapi"
)

// Handle is the wire boundary used by the plugin capability adapter. Principal
// is supplied by the trusted host call context, never decoded from browser JSON.
func (s *Service) Handle(ctx context.Context, principal portalapi.Principal, operation string, payload []byte) ([]byte, error) {
	var result any
	switch operation {
	case "createDraft":
		var spec portalapi.PortalSpec
		if err := decode(payload, &spec); err != nil {
			return nil, err
		}
		value, err := s.CreateDraft(ctx, principal, spec)
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
	for _, operation := range []string{"createDraft", "list", "submit", "approve", "publish", "rollback", "audit"} {
		op := operation
		handlers[op] = func(ctx context.Context, _ sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			principal, err := projectPrincipal(callCtx)
			if err != nil {
				return nil, nil, err
			}
			raw, err := service.Handle(ctx, principal, op, payload)
			if err != nil {
				return nil, nil, err
			}
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
		}
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: Descriptor(), Handlers: handlers}
}

func Descriptor() []byte {
	return []byte(`{"title":"门户组合治理","subcommands":[{"name":"createDraft","description":"创建 Portal 草稿"},{"name":"list","description":"列出 Portal revisions"},{"name":"submit","description":"提交草稿审批"},{"name":"approve","description":"审批草稿"},{"name":"publish","description":"发布 Portal revision"},{"name":"rollback","description":"回滚到历史 revision"},{"name":"audit","description":"读取 revision 审计"}]}`)
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
