// Package pluginreconcile owns the small target-specific strategy adapters for
// the shared plugin reconciliation contract. It does not perform scheduling,
// deployment, Portal assembly, Runner claiming or Mobile distribution.
package pluginreconcile

import (
	"errors"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func BackendAdapter() pluginv1.ReconciliationAdapter {
	return adapter{target: pluginv1.PluginTargetBackend, transition: backendTransition}
}
func FrontendAdapter() pluginv1.ReconciliationAdapter {
	return adapter{target: pluginv1.PluginTargetFrontend, transition: frontendTransition}
}
func RunnerAdapter() pluginv1.ReconciliationAdapter {
	return adapter{target: pluginv1.PluginTargetRunner, transition: profileTransition("runner.app-profile")}
}
func MobileAdapter() pluginv1.ReconciliationAdapter {
	return adapter{target: pluginv1.PluginTargetMobile, transition: profileTransition("mobile.bundle-publication")}
}

type adapter struct {
	target     string
	transition func(pluginv1.ReconciliationTransition) (string, error)
}

func (a adapter) Target() string { return a.target }
func (a adapter) Transition(value pluginv1.ReconciliationTransition) (string, error) {
	return a.transition(value)
}

func backendTransition(value pluginv1.ReconciliationTransition) (string, error) {
	switch value.Operation {
	case pluginv1.ReconcileActivate:
		return "backend.start-generation", nil
	case pluginv1.ReconcileReplace:
		return "backend.rolling-generation", nil
	case pluginv1.ReconcileDeactivate:
		return "backend.drain-stop", nil
	case pluginv1.ReconcileRetain:
		return "backend.retain", nil
	default:
		return "", errors.New("Backend 调和操作无效")
	}
}

func frontendTransition(value pluginv1.ReconciliationTransition) (string, error) {
	if value.Operation == pluginv1.ReconcileRetain {
		return "frontend.retain", nil
	}
	identity := value.Candidate
	if identity == nil {
		identity = value.Current
	}
	if identity == nil {
		return "", errors.New("Frontend 调和缺少制品身份")
	}
	for _, contribution := range value.Index.Contributions {
		if contribution.Owner.Ref.PluginID != identity.Ref.PluginID {
			continue
		}
		switch contribution.Kind {
		case "frontend.runtimeEngines", "frontend.renderAdapters", "frontend.rendererModules":
			return "frontend.host-epoch", nil
		}
	}
	return "frontend.portal-generation", nil
}

func profileTransition(prefix string) func(pluginv1.ReconciliationTransition) (string, error) {
	return func(value pluginv1.ReconciliationTransition) (string, error) {
		switch value.Operation {
		case pluginv1.ReconcileActivate, pluginv1.ReconcileReplace, pluginv1.ReconcileDeactivate:
			return prefix, nil
		case pluginv1.ReconcileRetain:
			return prefix + ".retain", nil
		default:
			return "", errors.New("App Profile 调和操作无效")
		}
	}
}
