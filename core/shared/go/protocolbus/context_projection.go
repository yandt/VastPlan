package protocolbus

import (
	"fmt"

	"cdsoft.com.cn/VastPlan/core/shared/go/callcontext"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
)

var allContextFields = callcontext.MustAccess(
	callcontext.FieldScopeTenant, callcontext.FieldScopeProject, callcontext.FieldCaller,
	callcontext.FieldScene, callcontext.FieldSubjectID, callcontext.FieldSubjectProfile,
	callcontext.FieldAuthorizationRole, callcontext.FieldAuthorizationAdmin,
	callcontext.FieldTrace, callcontext.FieldRequestDeadline, callcontext.FieldRequestIdempotency,
	callcontext.FieldGrantCredentials, callcontext.FieldBaggage, callcontext.FieldPropagationPath,
)

var undeclaredContextFields = callcontext.MustAccess(
	callcontext.FieldScopeTenant, callcontext.FieldScopeProject, callcontext.FieldCaller,
	callcontext.FieldScene, callcontext.FieldSubjectID, callcontext.FieldTrace,
	callcontext.FieldRequestDeadline, callcontext.FieldRequestIdempotency,
	callcontext.FieldPropagationPath,
)

func requestedContext(policy LaunchPolicy) (required, optional callcontext.AccessSet, err error) {
	access := policy.ContextAccess
	if len(access.Required) == 0 && len(access.Optional) == 0 {
		if policy.UnrestrictedContext {
			// Host.Launch is an explicit local-development trust decision.
			return callcontext.AccessSet{}, allContextFields.Clone(), nil
		}
		// Missing declaration never expands to a broad first-party ceiling.
		return callcontext.AccessSet{}, undeclaredContextFields.Clone(), nil
	}
	required, err = callcontext.ParseAccess(access.Required)
	if err != nil {
		return nil, nil, err
	}
	optional, err = callcontext.ParseAccess(access.Optional)
	if err != nil {
		return nil, nil, err
	}
	return required, optional, nil
}

func extensionPointContextCeiling(point string) callcontext.AccessSet {
	switch point {
	case extpoint.PermissionChecker:
		return callcontext.MustAccess(
			callcontext.FieldScopeTenant, callcontext.FieldScopeProject, callcontext.FieldCaller,
			callcontext.FieldScene, callcontext.FieldSubjectID, callcontext.FieldAuthorizationRole,
			callcontext.FieldAuthorizationAdmin, callcontext.FieldTrace,
			callcontext.FieldRequestDeadline, callcontext.FieldPropagationPath,
		)
	case extpoint.Hook:
		return callcontext.MustAccess(
			callcontext.FieldScopeTenant, callcontext.FieldScopeProject, callcontext.FieldCaller,
			callcontext.FieldScene, callcontext.FieldTrace, callcontext.FieldRequestDeadline,
			callcontext.FieldRequestIdempotency, callcontext.FieldPropagationPath,
		)
	case extpoint.EventSink:
		return callcontext.MustAccess(
			callcontext.FieldScopeTenant, callcontext.FieldCaller, callcontext.FieldScene,
			callcontext.FieldTrace, callcontext.FieldRequestDeadline, callcontext.FieldPropagationPath,
		)
	default:
		return allContextFields.Clone()
	}
}

func projectContextForPlugin(wire *contractv1.CallContext, target *contractv1.CallTarget, policy LaunchPolicy) (*contractv1.CallContext, error) {
	required, optional, err := requestedContext(policy)
	if err != nil {
		return nil, fmt.Errorf("插件 %s contextAccess: %w", policy.PluginID, err)
	}
	publisherCeiling := allContextFields
	if len(policy.ContextCeiling) != 0 {
		publisherCeiling, err = callcontext.ParseAccess(policy.ContextCeiling)
		if err != nil {
			return nil, fmt.Errorf("插件 %s 发布者上下文上限: %w", policy.PluginID, err)
		}
	}
	point := ""
	if target != nil {
		point = target.ExtensionPoint
	}
	projection, err := callcontext.EffectiveProjection(required, optional, policy.ContextAccess.Baggage,
		extensionPointContextCeiling(point), publisherCeiling)
	if err != nil {
		return nil, fmt.Errorf("插件 %s 上下文投影: %w", policy.PluginID, err)
	}
	trusted, err := callcontext.ValidateIngress(wire, callcontext.Provenance{
		Source: "protocolbus.host", AuthenticatedBy: "trusted-host", Audience: policy.PluginID,
	})
	if err != nil {
		return nil, fmt.Errorf("插件 %s 调用上下文无效: %w", policy.PluginID, err)
	}
	return trusted.Project(projection)
}
