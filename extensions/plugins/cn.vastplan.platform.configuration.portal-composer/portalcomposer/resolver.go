package portalcomposer

import (
	"fmt"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

func resolve(catalog frontendcompositionv1.PortalPlatformCatalog, application frontendcompositionv1.ApplicationComposition, tenantID string, revision uint64) (portalapi.PortalSpec, error) {
	if tenantID == "" || revision == 0 {
		return portalapi.PortalSpec{}, fmt.Errorf("Portal 解析需要 tenant 和发布 revision")
	}
	var err error
	catalog, err = frontendcompositionv1.ValidatePortalPlatformCatalog(catalog)
	if err != nil {
		return portalapi.PortalSpec{}, err
	}
	application, err = frontendcompositionv1.ValidateApplicationComposition(application)
	if err != nil {
		return portalapi.PortalSpec{}, err
	}
	profile, binding, err := catalog.Resolve(tenantID, application.ID)
	if err != nil {
		return portalapi.PortalSpec{}, err
	}
	origins := map[string]string{}
	plugins := make([]portalapi.PluginRef, 0, len(profile.Plugins)+len(application.Plugins))
	for _, ref := range profile.Plugins {
		origins[ref.ID] = compositioncommonv1.OriginPlatformProfile
		plugins = append(plugins, portalRef(ref))
	}
	for _, ref := range application.Plugins {
		if _, exists := origins[ref.ID]; exists {
			return portalapi.PortalSpec{}, fmt.Errorf("应用组合不能覆盖平台插件 %q", ref.ID)
		}
		origins[ref.ID] = compositioncommonv1.OriginApplication
		plugins = append(plugins, portalRef(ref))
	}
	return portalapi.PortalSpec{
		Revision: revision, ID: application.ID, TenantID: tenantID, Route: application.Route,
		Domains: append([]string(nil), application.Domains...), Audience: append([]string(nil), application.Audience...),
		Branding: cloneMap(application.Branding), Config: cloneMap(application.Config), Plugins: plugins,
		Localization:  localization(profile.Localization),
		Updates:       updates(profile.Updates),
		Management:    binding,
		RuntimeEngine: portalapi.RuntimeEngine{PluginRef: portalRef(profile.RuntimeEngine.PluginRef), EngineContract: profile.RuntimeEngine.EngineContract, Family: profile.RuntimeEngine.Family},
		RenderAdapter: portalapi.RenderAdapter{PluginRef: portalRef(profile.RenderAdapter.PluginRef), UIContract: profile.RenderAdapter.UIContract, Config: cloneRenderAdapterConfig(profile.RenderAdapter.Config)},
		Shell:         portalapi.Shell{PluginRef: portalRef(profile.Shell.PluginRef), UIContract: profile.Shell.UIContract, Config: profile.Shell.Config},
		Workbench:     portalapi.Workbench{PluginRef: portalRef(profile.Workbench.PluginRef), UIContract: profile.Workbench.UIContract, Config: cloneMap(profile.Workbench.Config)},
		Resolution: portalapi.Resolution{
			PlatformCatalog:         compositioncommonv1.Ref{ID: catalog.ID, Revision: catalog.Revision, Digest: catalog.Digest()},
			PlatformProfile:         compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()},
			ApplicationComposition:  compositioncommonv1.Ref{ID: application.ID, Revision: application.Revision, Digest: application.Digest()},
			ManagementBindingDigest: compositioncommonv1.Digest(binding),
			PluginOrigins:           origins,
		},
	}, nil
}

func updates(policy *frontendcompositionv1.UpdatePolicy) frontendcompositionv1.UpdatePolicy {
	if policy == nil || policy.Mode == "" {
		return frontendcompositionv1.UpdatePolicy{Mode: "refresh"}
	}
	return *policy
}

func localization(policy *frontendcompositionv1.LocalizationPolicy) frontendcompositionv1.LocalizationPolicy {
	if policy == nil {
		return frontendcompositionv1.LocalizationPolicy{DefaultLocale: "zh-CN", SupportedLocales: []string{"zh-CN", "en-US"}}
	}
	return frontendcompositionv1.LocalizationPolicy{DefaultLocale: policy.DefaultLocale, SupportedLocales: append([]string(nil), policy.SupportedLocales...)}
}

func portalRef(ref frontendcompositionv1.PluginRef) portalapi.PluginRef {
	return portalapi.PluginRef{ID: ref.ID, Version: ref.Version, Channel: ref.Channel}
}
