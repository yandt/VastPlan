package deploymentmanager

import (
	"strings"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
)

func authorizeInstallationSource(call *contractv1.CallContext, source plugininstallation.Source) error {
	if call == nil || !plugininstallation.ValidSource(source) {
		return plugininstallation.ErrUntrustedSource
	}
	switch source {
	case plugininstallation.SourceController, plugininstallation.SourceSelfService:
		if call.GetScene() != "portal.bff" || call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_USER {
			return plugininstallation.ErrUntrustedSource
		}
	case plugininstallation.SourceDevelopment:
		caller := call.GetCaller()
		trustedCaller := caller.GetKind() == contractv1.CallerKind_CALLER_KIND_SYSTEM &&
			(caller.GetId() == "platform-dev" || strings.HasPrefix(caller.GetId(), "platform-dev/"))
		if !trustedCaller {
			return plugininstallation.ErrDevelopmentForbidden
		}
	}
	return nil
}

func developmentInstallationBound(state *tenantState, request plugininstallation.PreviewRequest) bool {
	for _, binding := range state.TestBindings {
		if binding.Enabled && binding.Kind == platformadminapi.TestTargetBackend &&
			binding.Deployment == request.Target.Deployment && binding.UnitID == request.Target.UnitID &&
			binding.PluginID == request.Change.PluginID {
			return true
		}
	}
	return false
}
