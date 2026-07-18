// Package policy implements role-based Portal Composer authorization.
package policy

import (
	"context"
	"encoding/json"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

const PluginID = "com.vastplan.foundation.security.portal-access-policy"
const PluginVersion = "0.1.0"
const Capability = "foundation.security.portal-access-policy"

func Check(_ context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	var request extpoint.PermissionRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		return nil, nil, err
	}
	decision, reason := decide(callCtx, request)
	raw, err := json.Marshal(extpoint.PermissionResponse{Decision: decision, Reason: reason})
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func decide(c *contractv1.CallContext, request extpoint.PermissionRequest) (extpoint.Decision, string) {
	if c == nil || c.Caller == nil {
		return extpoint.DecisionDeny, "缺少经验证调用身份"
	}
	if c.Caller.Kind == contractv1.CallerKind_CALLER_KIND_PLUGIN && c.Caller.Id == PluginIDForComposer() && (request.Capability == "kernel.config.get" || request.Capability == portalapi.KernelCatalogValidationCapability) {
		return extpoint.DecisionAllow, "Composer 受限宿主回调"
	}
	if request.Capability != portalapi.ComposerCapability {
		return extpoint.DecisionAbstain, "非门户组合能力"
	}
	if c.Caller.Kind == contractv1.CallerKind_CALLER_KIND_SYSTEM {
		return extpoint.DecisionAllow, "系统 break-glass 调用"
	}
	if c.Caller.Kind != contractv1.CallerKind_CALLER_KIND_USER {
		return extpoint.DecisionDeny, "仅已认证用户可调用门户组合"
	}
	needed := map[string]string{"createDraft": "portal.compose", "submit": "portal.compose", "approve": "portal.approve", "publish": "portal.publish", "rollback": "portal.publish", "list": "portal.read", "audit": "portal.read"}[request.Operation]
	if needed == "" {
		return extpoint.DecisionDeny, "未知门户操作"
	}
	for _, role := range c.GetPrincipal().GetSystemRoles() {
		if role == needed || (needed == "portal.read" && (role == "portal.compose" || role == "portal.approve" || role == "portal.publish")) {
			return extpoint.DecisionAllow, "角色授权"
		}
	}
	return extpoint.DecisionDeny, "缺少门户角色"
}

func PluginIDForComposer() string { return "com.vastplan.platform.configuration.portal-composer" }
