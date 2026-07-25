package planner

import (
	"context"
	"fmt"
	"sort"
	"strings"

	semver "github.com/Masterminds/semver/v3"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type resolvedArtifacts struct {
	lock        pluginv1.ArtifactLock
	descriptors map[string]pluginv1.ArtifactPlanningDescriptor
	manifests   map[string]pluginv1.Manifest
	featureDeps map[string]map[string]map[string]string
	baselineIDs map[string]struct{}
}

func (s *Service) resolveArtifacts(ctx context.Context, repository Repository, intent backendcompositionv1.ApplicationIntent, profile backendcompositionv1.PlatformProfile) (resolvedArtifacts, error) {
	initialRefs, baselineIDs, err := planningRefs(intent, profile)
	if err != nil {
		return resolvedArtifacts{}, err
	}
	initial, err := repository.Describe(ctx, pluginv1.ArtifactPlanningRequest{Refs: initialRefs})
	if err != nil {
		return resolvedArtifacts{}, fmt.Errorf("读取根插件与 Platform Profile 规划描述: %w", err)
	}
	descriptors, manifests, err := descriptorMaps(initial.Items)
	if err != nil {
		return resolvedArtifacts{}, err
	}
	featureDeps, err := selectedFeatureDependencies(intent, manifests)
	if err != nil {
		return resolvedArtifacts{}, err
	}
	constraints := map[string][]string{}
	channels := map[string]string{}
	for _, service := range intent.Services {
		for _, root := range service.RootPlugins {
			constraints[root.Ref.PluginID] = append(constraints[root.Ref.PluginID], "="+root.Ref.Version)
			channels[root.Ref.PluginID] = root.Ref.Channel
		}
	}
	for _, baseline := range profile.ServiceBaselines {
		for _, ref := range baseline.Plugins {
			channel := ref.Channel
			if channel == "" {
				channel = "stable"
			}
			constraints[ref.ID] = append(constraints[ref.ID], "="+ref.Version)
			channels[ref.ID] = channel
		}
	}
	for _, roots := range featureDeps {
		for _, dependencies := range roots {
			for id, constraint := range dependencies {
				constraints[id] = append(constraints[id], constraint)
			}
		}
	}
	roots := make([]pluginv1.ArtifactRequirement, 0, len(constraints))
	for id, values := range constraints {
		sort.Strings(values)
		constraint := strings.Join(values, ", ")
		if _, err := semver.NewConstraint(constraint); err != nil {
			return resolvedArtifacts{}, fmt.Errorf("插件 %s 的组合版本约束无交集或无效: %s", id, constraint)
		}
		roots = append(roots, pluginv1.ArtifactRequirement{PluginID: id, Constraint: constraint, Channel: channels[id]})
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].PluginID < roots[j].PluginID })
	lock, err := repository.Resolve(ctx, pluginv1.ArtifactResolveRequest{
		Roots: roots, Target: "backend", KernelVersion: s.config.KernelVersion, Platform: s.config.Platform,
		AllowedChannels: append([]string(nil), s.config.AllowedChannels...), AllowedPublishers: append([]string(nil), s.config.AllowedPublishers...),
		AllowedPluginPrefixes: append([]string(nil), s.config.AllowedPluginPrefixes...), AvailableCapabilities: platformCapabilities(profile, manifests),
	})
	if err != nil {
		return resolvedArtifacts{}, fmt.Errorf("仓库解析应用制品闭包: %w", err)
	}
	lockedRefs := make([]pluginv1.ArtifactRef, 0, len(lock.Packages))
	for _, item := range lock.Packages {
		lockedRefs = append(lockedRefs, item.Ref)
	}
	locked, err := repository.Describe(ctx, pluginv1.ArtifactPlanningRequest{Refs: lockedRefs})
	if err != nil {
		return resolvedArtifacts{}, fmt.Errorf("读取 Artifact Lock 规划描述: %w", err)
	}
	lockedDescriptors, lockedManifests, err := descriptorMaps(locked.Items)
	if err != nil {
		return resolvedArtifacts{}, err
	}
	for _, item := range lock.Packages {
		descriptor, ok := lockedDescriptors[item.Ref.PluginID]
		if !ok || descriptor.Ref != item.Ref || descriptor.SHA256 != item.SHA256 || descriptor.Publisher != item.Publisher {
			return resolvedArtifacts{}, fmt.Errorf("Artifact Lock 与规划描述身份不一致: %s", item.Ref.PluginID)
		}
		descriptors[item.Ref.PluginID] = descriptor
		manifests[item.Ref.PluginID] = lockedManifests[item.Ref.PluginID]
	}
	return resolvedArtifacts{lock: lock, descriptors: descriptors, manifests: manifests, featureDeps: featureDeps, baselineIDs: baselineIDs}, nil
}

func planningRefs(intent backendcompositionv1.ApplicationIntent, profile backendcompositionv1.PlatformProfile) ([]pluginv1.ArtifactRef, map[string]struct{}, error) {
	refs := map[string]pluginv1.ArtifactRef{}
	baselineIDs := map[string]struct{}{}
	add := func(ref pluginv1.ArtifactRef) error {
		if current, exists := refs[ref.PluginID]; exists && current != ref {
			return fmt.Errorf("规划输入对插件 %s 使用了多个精确版本", ref.PluginID)
		}
		refs[ref.PluginID] = ref
		return nil
	}
	for _, service := range intent.Services {
		for _, root := range service.RootPlugins {
			if err := add(root.Ref); err != nil {
				return nil, nil, err
			}
		}
	}
	for _, baseline := range profile.ServiceBaselines {
		for _, ref := range baseline.Plugins {
			channel := ref.Channel
			if channel == "" {
				channel = "stable"
			}
			baselineIDs[ref.ID] = struct{}{}
			if err := add(pluginv1.ArtifactRef{PluginID: ref.ID, Version: ref.Version, Channel: channel}); err != nil {
				return nil, nil, err
			}
		}
	}
	for _, service := range profile.Services {
		for _, ref := range service.Plugins {
			channel := ref.Channel
			if channel == "" {
				channel = "stable"
			}
			if err := add(pluginv1.ArtifactRef{PluginID: ref.ID, Version: ref.Version, Channel: channel}); err != nil {
				return nil, nil, err
			}
		}
	}
	result := make([]pluginv1.ArtifactRef, 0, len(refs))
	for _, ref := range refs {
		result = append(result, ref)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].PluginID < result[j].PluginID })
	return result, baselineIDs, nil
}

func descriptorMaps(items []pluginv1.ArtifactPlanningDescriptor) (map[string]pluginv1.ArtifactPlanningDescriptor, map[string]pluginv1.Manifest, error) {
	descriptors := map[string]pluginv1.ArtifactPlanningDescriptor{}
	manifests := map[string]pluginv1.Manifest{}
	for _, item := range items {
		if current, exists := descriptors[item.Ref.PluginID]; exists && current.Ref != item.Ref {
			return nil, nil, fmt.Errorf("规划描述对插件 %s 返回多个版本", item.Ref.PluginID)
		}
		manifest, err := pluginv1.ParseManifest(item.Manifest)
		if err != nil {
			return nil, nil, err
		}
		descriptors[item.Ref.PluginID], manifests[item.Ref.PluginID] = item, manifest
	}
	return descriptors, manifests, nil
}

func selectedFeatureDependencies(intent backendcompositionv1.ApplicationIntent, manifests map[string]pluginv1.Manifest) (map[string]map[string]map[string]string, error) {
	result := map[string]map[string]map[string]string{}
	for _, service := range intent.Services {
		result[service.ID] = map[string]map[string]string{}
		for _, root := range service.RootPlugins {
			manifest, ok := manifests[root.Ref.PluginID]
			if !ok {
				return nil, fmt.Errorf("缺少根插件 %s 的规划描述", root.Ref.PluginID)
			}
			available := map[string]pluginv1.CompositionFeature{}
			if manifest.Composition != nil {
				for _, feature := range manifest.Composition.Features {
					available[feature.ID] = feature
				}
			}
			for _, id := range root.Features {
				feature, exists := available[id]
				if !exists {
					return nil, fmt.Errorf("根插件 %s 未声明 Feature %s", root.Ref.PluginID, id)
				}
				if result[service.ID][root.Ref.PluginID] == nil {
					result[service.ID][root.Ref.PluginID] = map[string]string{}
				}
				for dependency, constraint := range feature.Dependencies {
					if current := result[service.ID][root.Ref.PluginID][dependency]; current != "" && current != constraint {
						result[service.ID][root.Ref.PluginID][dependency] = current + ", " + constraint
					} else {
						result[service.ID][root.Ref.PluginID][dependency] = constraint
					}
				}
			}
		}
	}
	return result, nil
}

func platformCapabilities(profile backendcompositionv1.PlatformProfile, manifests map[string]pluginv1.Manifest) []pluginv1.AvailableCapability {
	seen := map[string]struct{}{}
	var result []pluginv1.AvailableCapability
	for _, service := range profile.Services {
		for _, ref := range service.Plugins {
			manifest, ok := manifests[ref.ID]
			if !ok {
				continue
			}
			contributions, _ := pluginv1.BackendRuntimeContributions(manifest)
			for _, contribution := range contributions {
				key := contribution.ID + "\x00" + manifest.Version
				if _, duplicate := seen[key]; duplicate {
					continue
				}
				seen[key] = struct{}{}
				result = append(result, pluginv1.AvailableCapability{Capability: contribution.ID, Version: manifest.Version})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Capability != result[j].Capability {
			return result[i].Capability < result[j].Capability
		}
		return result[i].Version < result[j].Version
	})
	return result
}
