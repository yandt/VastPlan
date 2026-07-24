package compositionresolver

import (
	"encoding/json"
	"fmt"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/compositioncore"
)

type ArtifactReader = compositioncore.ArtifactReader
type Options = compositioncore.Options

// Resolve verifies every exact artifact before it uses publisher and namespace
// classification. Platform-origin plugins may include administrator-promoted
// application plugins; application input can never select platform plugins.
func Resolve(profile backendcompositionv1.PlatformProfile, application backendcompositionv1.ApplicationComposition, deploymentRevision uint64, artifacts ArtifactReader, options Options) (deploymentv2.Deployment, error) {
	if deploymentRevision == 0 {
		return deploymentv2.Deployment{}, fmt.Errorf("deployment revision 必须大于 0")
	}
	if artifacts == nil {
		return deploymentv2.Deployment{}, fmt.Errorf("Composition Resolver 必须配置不可变制品读取器")
	}
	var err error
	profile, err = backendcompositionv1.ValidatePlatformProfile(profile)
	if err != nil {
		return deploymentv2.Deployment{}, err
	}
	application, err = backendcompositionv1.ValidateApplicationComposition(application)
	if err != nil {
		return deploymentv2.Deployment{}, err
	}

	allowedClasses := make(map[string]struct{}, len(profile.ServiceClasses))
	for _, serviceClass := range profile.ServiceClasses {
		allowedClasses[serviceClass] = struct{}{}
	}
	for _, unit := range application.Units {
		if _, ok := allowedClasses[unit.ServiceClass]; !ok {
			return deploymentv2.Deployment{}, fmt.Errorf("应用 unit %q 使用平台未允许的 serviceClass %q", unit.Spec.ID, unit.ServiceClass)
		}
	}

	platformRefs := map[string]compositioncore.Selection{}
	baselinePluginIDs := map[string]struct{}{}
	pluginBaselines := map[string]string{}
	for _, baseline := range profile.ServiceBaselines {
		for _, ref := range baseline.Plugins {
			if err := compositioncore.VerifyRef(selection(ref), compositioncommonv1.OriginPlatformProfile, platformRefs, artifacts, options); err != nil {
				return deploymentv2.Deployment{}, fmt.Errorf("Platform Profile service baseline %s: %w", baseline.ID, err)
			}
			baselinePluginIDs[ref.ID] = struct{}{}
			pluginBaselines[ref.ID] = baseline.ID
		}
	}
	servicePluginUnits := map[string]string{}
	for _, unit := range profile.Services {
		for _, ref := range unit.Plugins {
			if _, baseline := baselinePluginIDs[ref.ID]; baseline {
				return deploymentv2.Deployment{}, fmt.Errorf("平台插件 %q 不能同时属于公共 service baseline 和独立 seed service", ref.ID)
			}
			if previousUnit := servicePluginUnits[ref.ID]; previousUnit != "" && previousUnit != unit.ID {
				reusable, err := reusableLocalPermissionPlugin(ref, artifacts)
				if err != nil {
					return deploymentv2.Deployment{}, err
				}
				if !reusable {
					return deploymentv2.Deployment{}, fmt.Errorf("平台插件 %q 不能同时属于 service unit %q 和 %q", ref.ID, previousUnit, unit.ID)
				}
			}
			if err := compositioncore.VerifyRef(selection(ref), compositioncommonv1.OriginPlatformProfile, platformRefs, artifacts, options); err != nil {
				return deploymentv2.Deployment{}, fmt.Errorf("Platform Profile service %s: %w", unit.ID, err)
			}
			servicePluginUnits[ref.ID] = unit.ID
		}
	}

	applicationRefs := map[string]compositioncore.Selection{}
	for _, unit := range application.Units {
		for _, ref := range unit.Spec.Plugins {
			if _, platformOwned := platformRefs[ref.ID]; platformOwned {
				return deploymentv2.Deployment{}, fmt.Errorf("应用 unit %q 不能覆盖平台插件 %q", unit.Spec.ID, ref.ID)
			}
			if err := compositioncore.VerifyRef(selection(ref), compositioncommonv1.OriginApplication, applicationRefs, artifacts, options); err != nil {
				return deploymentv2.Deployment{}, fmt.Errorf("Application Composition unit %s: %w", unit.Spec.ID, err)
			}
		}
	}

	baselinePlugins := map[string][]deploymentv1.PluginRef{}
	baselineConfigs := map[string]map[string]any{}
	for _, baseline := range profile.ServiceBaselines {
		baselinePlugins[baseline.ServiceClass] = append(baselinePlugins[baseline.ServiceClass], baseline.Plugins...)
		merged, err := compositioncore.MergeProtectedConfig(baselineConfigs[baseline.ServiceClass], baseline.Config)
		if err != nil {
			return deploymentv2.Deployment{}, fmt.Errorf("合并 service class %q 的公共基线 %q: %w", baseline.ServiceClass, baseline.ID, err)
		}
		baselineConfigs[baseline.ServiceClass] = merged
	}
	units := make([]deploymentv2.ServiceUnit, 0, len(application.Units)+len(profile.Services))
	unitIDs := map[string]struct{}{}
	for _, applicationUnit := range application.Units {
		unit := applicationUnit.Spec
		injected := append([]deploymentv1.PluginRef(nil), baselinePlugins[applicationUnit.ServiceClass]...)
		unit.Plugins = append(injected, unit.Plugins...)
		unit.Config, err = compositioncore.MergeProtectedConfig(baselineConfigs[applicationUnit.ServiceClass], unit.Config)
		if err != nil {
			return deploymentv2.Deployment{}, fmt.Errorf("应用 unit %q 的服务配置与公共基线冲突: %w", unit.ID, err)
		}
		if _, duplicate := unitIDs[unit.ID]; duplicate {
			return deploymentv2.Deployment{}, fmt.Errorf("解析后 unit id 重复: %q", unit.ID)
		}
		unitIDs[unit.ID] = struct{}{}
		units = append(units, unit)
	}
	for _, platformUnit := range profile.Services {
		if _, duplicate := unitIDs[platformUnit.ID]; duplicate {
			return deploymentv2.Deployment{}, fmt.Errorf("平台 service unit %q 与应用 unit 冲突", platformUnit.ID)
		}
		unitIDs[platformUnit.ID] = struct{}{}
		units = append(units, platformUnit)
	}

	origins := make(map[string]string, len(platformRefs)+len(applicationRefs))
	for id := range platformRefs {
		origins[id] = compositioncommonv1.OriginPlatformProfile
	}
	for id := range applicationRefs {
		origins[id] = compositioncommonv1.OriginApplication
	}
	resolved := deploymentv2.Deployment{
		Version: 2, Revision: deploymentRevision, Metadata: application.Metadata,
		Resolution: deploymentv2.Resolution{
			PlatformProfile:        deploymentv2.CompositionRef{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()},
			ApplicationComposition: deploymentv2.CompositionRef{ID: application.ID, Revision: application.Revision, Digest: application.Digest()},
			DevelopmentMode:        options.AllowDevelopmentPlugins,
			PluginOrigins:          origins,
			PluginBaselines:        pluginBaselines,
		},
		Units: units,
	}
	raw, err := json.Marshal(resolved)
	if err != nil {
		return deploymentv2.Deployment{}, fmt.Errorf("编码解析后的 Deployment: %w", err)
	}
	resolved, err = deploymentv2.Parse(raw)
	if err != nil {
		return deploymentv2.Deployment{}, fmt.Errorf("解析后的 Deployment 无效: %w", err)
	}
	return resolved, nil
}

// reusableLocalPermissionPlugin permits one exact, immutable authorization
// plugin to guard multiple platform service hosts. It must contribute only
// local permission checkers; a shared/cluster capability remains single-owner.
func reusableLocalPermissionPlugin(ref deploymentv1.PluginRef, artifacts ArtifactReader) (bool, error) {
	channel := ref.Channel
	if channel == "" {
		channel = "stable"
	}
	artifact, _, err := artifacts.Read(pluginv1.ArtifactRef{PluginID: ref.ID, Version: ref.Version, Channel: channel})
	if err != nil {
		return false, fmt.Errorf("读取可复用本地权限插件 %s@%s/%s: %w", ref.ID, ref.Version, channel, err)
	}
	manifest, err := pluginv1.ParseManifest(artifact.Manifest)
	if err != nil {
		return false, fmt.Errorf("解析可复用本地权限插件 %s: %w", ref.ID, err)
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil {
		return false, fmt.Errorf("解析可复用本地权限插件 %s runtime: %w", ref.ID, err)
	}
	if len(contributions) == 0 {
		return false, nil
	}
	for _, contribution := range contributions {
		if !pluginv1.IsLocalPermissionAuxiliary(contribution) {
			return false, nil
		}
	}
	return true, nil
}

func selection(ref deploymentv1.PluginRef) compositioncore.Selection {
	return compositioncore.Selection{ID: ref.ID, Version: ref.Version, Channel: ref.Channel}
}
