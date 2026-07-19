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
		Management:   binding,
		DesignSystem: portalapi.DesignSystem{PluginRef: portalRef(profile.DesignSystem.PluginRef), UIContract: profile.DesignSystem.UIContract},
		Composition:  portalapi.ShellComposition{PluginRef: portalRef(profile.Composition.PluginRef), UIContract: profile.Composition.UIContract, Config: profile.Composition.Config},
		Layout:       portalapi.ShellLayout{PluginRef: portalRef(profile.Layout.PluginRef), UIContract: profile.Layout.UIContract, Config: cloneMap(profile.Layout.Config)},
		Resolution: portalapi.Resolution{
			PlatformCatalog:         compositioncommonv1.Ref{ID: catalog.ID, Revision: catalog.Revision, Digest: catalog.Digest()},
			PlatformProfile:         compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()},
			ApplicationComposition:  compositioncommonv1.Ref{ID: application.ID, Revision: application.Revision, Digest: application.Digest()},
			ManagementBindingDigest: compositioncommonv1.Digest(binding),
			PluginOrigins:           origins,
		},
	}, nil
}

func portalRef(ref frontendcompositionv1.PluginRef) portalapi.PluginRef {
	return portalapi.PluginRef{ID: ref.ID, Version: ref.Version, Channel: ref.Channel}
}
