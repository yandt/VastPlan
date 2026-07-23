// Package policy implements role-based Portal Composer authorization.
package policy

import (
	"context"
	"encoding/json"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

const PluginID = "cn.vastplan.foundation.security.portal-access-policy"
const PluginVersion = "0.4.0"
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
	if c.Caller.Kind == contractv1.CallerKind_CALLER_KIND_PLUGIN && c.Caller.Id == PluginIDForComposer() && (request.Capability == "kernel.config.get" || request.Capability == portalapi.KernelCatalogValidationCapability || request.Capability == portalapi.KernelCatalogMaterializationCapability || request.Capability == portalapi.KernelArtifactReferencePublicationCapability || request.Capability == portalapi.KernelTestArtifactValidationCapability || composerSharedStateCapability(request.Capability)) {
		return extpoint.DecisionAllow, "Composer 受限宿主回调"
	}
	if request.Capability == portalapi.PreferenceCapability {
		if c.Caller.Kind == contractv1.CallerKind_CALLER_KIND_USER && c.GetScene() == "portal.bff" && c.GetPrincipal().GetUserId() != "" {
			if request.Operation == "get" || request.Operation == "put" {
				return extpoint.DecisionAllow, "当前主体 Portal 偏好"
			}
			return extpoint.DecisionDeny, "未知 PortalPreference 操作"
		}
		return extpoint.DecisionDeny, "PortalPreference 只允许可信 Portal BFF 用户场景"
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
	needed := map[string][]string{
		"createDraft": {"portal.compose"}, "updateDraft": {"portal.compose"}, "submit": {"portal.compose"},
		"approve": {"portal.approve"}, "publish": {"portal.publish"},
		"createProfileDraft": {"portal.compose"}, "updateProfileDraft": {"portal.compose"},
		"createBindingDraft": {"portal.compose"}, "updateBindingDraft": {"portal.compose"},
		// Transition payloads are decoded and finally authorized by Composer;
		// this outer policy admits only the three lifecycle roles.
		"transitionProfile": {"portal.compose", "portal.approve", "portal.publish"},
		"transitionBinding": {"portal.compose", "portal.approve", "portal.publish"},
		"activate":          {"portal.publish"}, "rollbackActivation": {"portal.publish"},
		"putTestTargetBinding": {"portal.compose"},
		"createTestRelease":    {"portal.publish"}, "rollbackTestRelease": {"portal.publish"},
		"list":                   {"portal.read", "portal.compose", "portal.approve", "portal.publish"},
		"audit":                  {"portal.read", "portal.compose", "portal.approve", "portal.publish"},
		"governance":             {"portal.read", "portal.compose", "portal.approve", "portal.publish"},
		"listActivations":        {"portal.read", "portal.compose", "portal.approve", "portal.publish"},
		"listTestTargetBindings": {"portal.read", "portal.compose", "portal.publish"},
		"listTestReleases":       {"portal.read", "portal.compose", "portal.publish"},
	}[request.Operation]
	if len(needed) == 0 {
		return extpoint.DecisionDeny, "未知门户操作"
	}
	for _, actual := range c.GetPrincipal().GetSystemRoles() {
		for _, allowed := range needed {
			if actual == allowed {
				return extpoint.DecisionAllow, "角色授权"
			}
		}
	}
	return extpoint.DecisionDeny, "缺少门户角色"
}

func composerSharedStateCapability(capability string) bool {
	return capability == "kernel.state.shared.get" || capability == "kernel.state.shared.create" || capability == "kernel.state.shared.update"
}

func PluginIDForComposer() string { return "cn.vastplan.platform.configuration.portal-composer" }
