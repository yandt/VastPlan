package compositionresolver

import (
	"encoding/json"
	"fmt"

	compositionv1 "cdsoft.com.cn/VastPlan/schemas/composition/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/shared/go/pluginid"
)

type ArtifactReader interface {
	Read(pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, error)
}

type Options struct {
	AllowDevelopmentPlugins bool
}

// Resolve verifies every exact artifact before it uses publisher and namespace
// classification. Platform-origin plugins may include administrator-promoted
// application plugins; application input can never select platform plugins.
func Resolve(profile compositionv1.PlatformProfile, application compositionv1.ApplicationComposition, deploymentRevision uint64, artifacts ArtifactReader, options Options) (deploymentv2.Deployment, error) {
	if deploymentRevision == 0 {
		return deploymentv2.Deployment{}, fmt.Errorf("deployment revision 必须大于 0")
	}
	if artifacts == nil {
		return deploymentv2.Deployment{}, fmt.Errorf("Composition Resolver 必须配置不可变制品读取器")
	}
	var err error
	profile, err = compositionv1.ValidatePlatformProfile(profile)
	if err != nil {
		return deploymentv2.Deployment{}, err
	}
	application, err = compositionv1.ValidateApplicationComposition(application)
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

	platformRefs := map[string]deploymentv1.PluginRef{}
	attachmentPluginIDs := map[string]struct{}{}
	for _, attachment := range profile.Attachments {
		for _, ref := range attachment.Plugins {
			if err := verifyRef(ref, deploymentv2.OriginPlatformProfile, platformRefs, artifacts, options); err != nil {
				return deploymentv2.Deployment{}, fmt.Errorf("Platform Profile attachment %s: %w", attachment.ServiceClass, err)
			}
			attachmentPluginIDs[ref.ID] = struct{}{}
		}
	}
	servicePluginUnits := map[string]string{}
	for _, unit := range profile.Services {
		for _, ref := range unit.Plugins {
			if _, attached := attachmentPluginIDs[ref.ID]; attached {
				return deploymentv2.Deployment{}, fmt.Errorf("平台插件 %q 不能同时作为本地 attachment 和独立 service", ref.ID)
			}
			if previousUnit := servicePluginUnits[ref.ID]; previousUnit != "" && previousUnit != unit.ID {
				return deploymentv2.Deployment{}, fmt.Errorf("平台插件 %q 不能同时属于 service unit %q 和 %q", ref.ID, previousUnit, unit.ID)
			}
			if err := verifyRef(ref, deploymentv2.OriginPlatformProfile, platformRefs, artifacts, options); err != nil {
				return deploymentv2.Deployment{}, fmt.Errorf("Platform Profile service %s: %w", unit.ID, err)
			}
			servicePluginUnits[ref.ID] = unit.ID
		}
	}

	applicationRefs := map[string]deploymentv1.PluginRef{}
	for _, unit := range application.Units {
		for _, ref := range unit.Spec.Plugins {
			if _, platformOwned := platformRefs[ref.ID]; platformOwned {
				return deploymentv2.Deployment{}, fmt.Errorf("应用 unit %q 不能覆盖平台插件 %q", unit.Spec.ID, ref.ID)
			}
			if err := verifyRef(ref, deploymentv2.OriginApplication, applicationRefs, artifacts, options); err != nil {
				return deploymentv2.Deployment{}, fmt.Errorf("Application Composition unit %s: %w", unit.Spec.ID, err)
			}
		}
	}

	attachments := map[string][]deploymentv1.PluginRef{}
	for _, attachment := range profile.Attachments {
		attachments[attachment.ServiceClass] = append(attachments[attachment.ServiceClass], attachment.Plugins...)
	}
	units := make([]deploymentv2.ServiceUnit, 0, len(application.Units)+len(profile.Services))
	unitIDs := map[string]struct{}{}
	for _, applicationUnit := range application.Units {
		unit := applicationUnit.Spec
		injected := append([]deploymentv1.PluginRef(nil), attachments[applicationUnit.ServiceClass]...)
		unit.Plugins = append(injected, unit.Plugins...)
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
		origins[id] = deploymentv2.OriginPlatformProfile
	}
	for id := range applicationRefs {
		origins[id] = deploymentv2.OriginApplication
	}
	resolved := deploymentv2.Deployment{
		Version: 2, Revision: deploymentRevision, Metadata: application.Metadata,
		Resolution: deploymentv2.Resolution{
			PlatformProfile:        deploymentv2.CompositionRef{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()},
			ApplicationComposition: deploymentv2.CompositionRef{ID: application.ID, Revision: application.Revision, Digest: application.Digest()},
			DevelopmentMode:        options.AllowDevelopmentPlugins,
			PluginOrigins:          origins,
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

func verifyRef(ref deploymentv1.PluginRef, origin string, seen map[string]deploymentv1.PluginRef, artifacts ArtifactReader, options Options) error {
	ref.Channel = normalizedChannel(ref.Channel)
	if previous, ok := seen[ref.ID]; ok {
		if previous.Version != ref.Version || normalizedChannel(previous.Channel) != ref.Channel {
			return fmt.Errorf("插件 %q 存在多版本或多 channel 冲突", ref.ID)
		}
		return nil
	}
	artifact, _, err := artifacts.Read(pluginv1.ArtifactRef{PluginID: ref.ID, Version: ref.Version, Channel: ref.Channel})
	if err != nil {
		return fmt.Errorf("读取制品 %s@%s/%s: %w", ref.ID, ref.Version, ref.Channel, err)
	}
	manifest, err := pluginv1.ParseManifest(artifact.Manifest)
	if err != nil {
		return fmt.Errorf("制品 %s 清单无效: %w", ref.ID, err)
	}
	if artifact.PluginID != ref.ID || artifact.Version != ref.Version || normalizedChannel(artifact.Channel) != ref.Channel || manifest.ID != ref.ID || manifest.Version != ref.Version {
		return fmt.Errorf("制品引用与不可变清单身份不一致: %s@%s/%s", ref.ID, ref.Version, ref.Channel)
	}
	class, err := pluginid.ClassifyManagement(manifest.ID, manifest.Publisher)
	if err != nil {
		return fmt.Errorf("插件 %s 身份分类失败: %w", ref.ID, err)
	}
	if class == pluginid.ManagementDevelopment && !options.AllowDevelopmentPlugins {
		return fmt.Errorf("开发插件 %q 未被生产策略允许", ref.ID)
	}
	if origin == deploymentv2.OriginApplication && class == pluginid.ManagementPlatform {
		return fmt.Errorf("应用配置不能选择平台管理插件 %q", ref.ID)
	}
	seen[ref.ID] = ref
	return nil
}

func normalizedChannel(channel string) string {
	if channel == "" {
		return "stable"
	}
	return channel
}
