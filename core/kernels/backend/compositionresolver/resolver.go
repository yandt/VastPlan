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

	platformRefs := map[string]compositioncore.ResolvedArtifact{}
	baselinePluginIDs := map[string]struct{}{}
	pluginBaselines := map[string]string{}
	for _, baseline := range profile.ServiceBaselines {
		for _, ref := range baseline.Plugins {
			if _, err := compositioncore.ResolveRef(selection(ref), compositioncommonv1.OriginPlatformProfile, platformRefs, artifacts, options); err != nil {
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
			resolvedArtifact, err := compositioncore.ResolveRef(selection(ref), compositioncommonv1.OriginPlatformProfile, platformRefs, artifacts, options)
			if err != nil {
				return deploymentv2.Deployment{}, fmt.Errorf("Platform Profile service %s: %w", unit.ID, err)
			}
			if previousUnit := servicePluginUnits[ref.ID]; previousUnit != "" && previousUnit != unit.ID {
				reusable, err := reusableHostLocalPlugin(resolvedArtifact.Manifest)
				if err != nil {
					return deploymentv2.Deployment{}, err
				}
				if !reusable {
					return deploymentv2.Deployment{}, fmt.Errorf("平台插件 %q 不能同时属于 service unit %q 和 %q", ref.ID, previousUnit, unit.ID)
				}
			}
			servicePluginUnits[ref.ID] = unit.ID
		}
	}

	applicationRefs := map[string]compositioncore.ResolvedArtifact{}
	for _, unit := range application.Units {
		for _, ref := range unit.Spec.Plugins {
			if _, platformOwned := platformRefs[ref.ID]; platformOwned {
				return deploymentv2.Deployment{}, fmt.Errorf("应用 unit %q 不能覆盖平台插件 %q", unit.Spec.ID, ref.ID)
			}
			if _, err := compositioncore.ResolveRef(selection(ref), compositioncommonv1.OriginApplication, applicationRefs, artifacts, options); err != nil {
				return deploymentv2.Deployment{}, fmt.Errorf("Application Composition unit %s: %w", unit.Spec.ID, err)
			}
		}
	}

	baselinePlugins := map[string][]deploymentv1.PluginRef{}
	baselineConfigs := map[string]map[string]any{}
	for _, baseline := range profile.ServiceBaselines {
		resolved, err := resolvedPluginRefs(baseline.Plugins, platformRefs)
		if err != nil {
			return deploymentv2.Deployment{}, err
		}
		baselinePlugins[baseline.ServiceClass] = append(baselinePlugins[baseline.ServiceClass], resolved...)
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
		unit.Plugins, err = resolvedPluginRefs(unit.Plugins, applicationRefs)
		if err != nil {
			return deploymentv2.Deployment{}, err
		}
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
		platformUnit.Plugins, err = resolvedPluginRefs(platformUnit.Plugins, platformRefs)
		if err != nil {
			return deploymentv2.Deployment{}, err
		}
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

// reusableHostLocalPlugin permits an exact immutable per-kernel sidecar to be
// instantiated in multiple service hosts. The rule is topology-based rather
// than tied to a named permission plugin family; any shared/cluster
// contribution remains single-owner.
func reusableHostLocalPlugin(manifest pluginv1.Manifest) (bool, error) {
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil {
		return false, fmt.Errorf("解析可复用 host-local 插件 %s runtime: %w", manifest.ID, err)
	}
	if len(contributions) == 0 {
		return false, nil
	}
	for _, contribution := range contributions {
		if contribution.InstancePolicy != "per-kernel" || contribution.StateModel != "local-ephemeral" ||
			contribution.Visibility != "local" || contribution.Routing != "direct" || contribution.RoutingDomain != "" {
			return false, nil
		}
	}
	return true, nil
}

func selection(ref deploymentv1.PluginRef) compositioncore.Selection {
	return compositioncore.Selection{ID: ref.ID, Version: ref.Version, Channel: ref.Channel, SHA256: ref.SHA256}
}

func resolvedPluginRefs(refs []deploymentv1.PluginRef, selections map[string]compositioncore.ResolvedArtifact) ([]deploymentv1.PluginRef, error) {
	resolved := make([]deploymentv1.PluginRef, len(refs))
	copy(resolved, refs)
	for index := range resolved {
		artifact, ok := selections[resolved[index].ID]
		if !ok || artifact.Selection.SHA256 == "" {
			return nil, fmt.Errorf("插件 %s 缺少 Resolver 精确 SHA-256", resolved[index].ID)
		}
		resolved[index].Channel = compositioncore.NormalizeChannel(resolved[index].Channel)
		resolved[index].SHA256 = artifact.Selection.SHA256
	}
	return resolved, nil
}
