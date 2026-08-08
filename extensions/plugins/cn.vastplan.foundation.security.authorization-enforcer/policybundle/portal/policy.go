// Package portal implements Portal authorization inside the shared
// Authorization Enforcer process.
package portal

import (
	"context"
	"encoding/json"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

const Capability = "foundation.security.portal-access-policy"

const (
	authenticationBrokerCapability = authenticationv1.BrokerCapability
	authorizationSessionCapability = "foundation.security.authorization-session"
	authenticationBrokerPluginID   = authenticationv1.BrokerPluginID
)

var authenticationBrokerOperations = map[string]struct{}{
	"describe": {}, "begin": {}, "continue": {}, "resend": {}, "cancel": {},
	"health": {}, "consumeAssertion": {}, "beginProviderTest": {},
}

var authenticationProviderOperations = map[string]struct{}{
	"describe": {}, "begin": {}, "continue": {}, "resend": {}, "cancel": {}, "health": {},
}

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
	if request.ExtensionPoint == extpoint.AuthenticationProvider {
		if c.Caller.Kind != contractv1.CallerKind_CALLER_KIND_PLUGIN || c.Caller.Id != authenticationBrokerPluginID {
			return extpoint.DecisionDeny, "Authentication Provider 只允许认证 Broker 调用"
		}
		if _, ok := authenticationProviderOperations[request.Operation]; ok {
			return extpoint.DecisionAllow, "认证 Broker 调用受治理 Method Provider"
		}
		return extpoint.DecisionDeny, "未知 Authentication Provider 操作"
	}
	if request.Capability == authenticationBrokerCapability || request.Capability == authorizationSessionCapability {
		if c.Caller.Kind != contractv1.CallerKind_CALLER_KIND_SYSTEM || c.Caller.Id != "portal-host" || c.GetScene() != "portal.bff" {
			return extpoint.DecisionDeny, "认证基础能力只允许受信 Portal BFF workload"
		}
		if request.Capability == authorizationSessionCapability {
			if request.Operation == "resolve" {
				return extpoint.DecisionAllow, "受信 Portal BFF 解析登录会话授权"
			}
			return extpoint.DecisionDeny, "未知 Authorization Session 操作"
		}
		if _, ok := authenticationBrokerOperations[request.Operation]; ok {
			return extpoint.DecisionAllow, "受信 Portal BFF 调用认证 Broker"
		}
		return extpoint.DecisionDeny, "未知 Authentication Broker 操作"
	}
	if c.Caller.Kind == contractv1.CallerKind_CALLER_KIND_PLUGIN && c.Caller.Id == PluginIDForComposer() && (request.Capability == "kernel.config.get" || request.Capability == portalapi.KernelCatalogBrowserExposureCompilationCapability || request.Capability == portalapi.KernelCatalogValidationCapability || request.Capability == portalapi.KernelCatalogMaterializationCapability || request.Capability == portalapi.KernelArtifactReferencePublicationCapability || request.Capability == portalapi.KernelTestArtifactValidationCapability || composerSharedStateCapability(request.Capability)) {
		return extpoint.DecisionAllow, "Composer 受限宿主回调"
	}
	if c.Caller.Kind == contractv1.CallerKind_CALLER_KIND_PLUGIN && c.Caller.Id == PluginIDForComposer() && request.Capability == workspacev1.Capability {
		return extpoint.DecisionAllow, "Composer 可选版本控制端口"
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
	if c.Caller.Kind == contractv1.CallerKind_CALLER_KIND_USER {
		return extpoint.DecisionAbstain, "用户 Portal Composer 操作由签名 Permission Catalog 判定"
	}
	return extpoint.DecisionDeny, "仅已认证用户可调用门户组合"
}

func composerSharedStateCapability(capability string) bool {
	return capability == "kernel.state.shared.get" || capability == "kernel.state.shared.create" || capability == "kernel.state.shared.update"
}

func PluginIDForComposer() string { return "cn.vastplan.platform.configuration.portal-composer" }
