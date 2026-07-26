package planner

import (
	"fmt"
	"sort"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginid"
)

type plannedUnit struct {
	service       backendcompositionv1.ServiceIntent
	application   []string
	installed     []string
	paths         map[string][]string
	featureByRoot map[string][]pluginv1.CompositionFeature
	unit          backendcompositionv1.ApplicationUnit
}

func (s *Service) compile(intent backendcompositionv1.ApplicationIntent, profile backendcompositionv1.PlatformProfile, artifacts resolvedArtifacts, credentials credentialBindings) (compileResult, error) {
	allowedClasses := map[string]struct{}{}
	for _, class := range profile.ServiceClasses {
		allowedClasses[class] = struct{}{}
	}
	lockPackages := map[string]pluginv1.ArtifactLockPackage{}
	for _, item := range artifacts.lock.Packages {
		lockPackages[item.Ref.PluginID] = item
	}
	baselineByClass := map[string][]string{}
	for _, baseline := range profile.ServiceBaselines {
		for _, ref := range baseline.Plugins {
			baselineByClass[baseline.ServiceClass] = append(baselineByClass[baseline.ServiceClass], ref.ID)
		}
	}
	var units []plannedUnit
	var features []backendcompositionv1.ResolvedFeature
	var configuration []backendcompositionv1.ConfigurationPlanItem
	for _, service := range intent.Services {
		if _, allowed := allowedClasses[service.ServiceClass]; !allowed {
			return compileResult{}, fmt.Errorf("service %s 使用 Platform Profile 未允许的 serviceClass %s", service.ID, service.ServiceClass)
		}
		application, paths, err := serviceClosure(service, artifacts, lockPackages)
		if err != nil {
			return compileResult{}, err
		}
		if err := s.validateApplicationPlugins(application, artifacts); err != nil {
			return compileResult{}, fmt.Errorf("service %s: %w", service.ID, err)
		}
		installed := append([]string(nil), baselineByClass[service.ServiceClass]...)
		installed = append(installed, application...)
		sort.Strings(installed)
		policy, err := policyForUnit(installed, artifacts.manifests)
		if err != nil {
			return compileResult{}, fmt.Errorf("service %s: %w", service.ID, err)
		}
		if policy.InstancePolicy == "partitioned" {
			return compileResult{}, fmt.Errorf("service %s 使用 partitioned 策略，但 Application Intent v1 不允许用户提供 partition keys", service.ID)
		}
		pluginRefs := make([]deploymentv1.PluginRef, 0, len(application))
		for _, id := range application {
			locked := lockPackages[id]
			pluginRefs = append(pluginRefs, deploymentv1.PluginRef{ID: id, Version: locked.Ref.Version, Channel: locked.Ref.Channel, SHA256: locked.SHA256})
		}
		serviceCredentials := credentials[service.ID]
		config, err := applicationConfig(service.PluginConfig, serviceCredentials, application, artifacts.manifests)
		if err != nil {
			return compileResult{}, fmt.Errorf("service %s: %w", service.ID, err)
		}
		spec := deploymentv2.ServiceUnit{
			ID: service.ID, Kind: "service", Plugins: pluginRefs, Config: config, Enabled: true, ServiceRole: "backend",
			LogicalService: intent.ID + "." + service.ID,
			InstancePolicy: policy.InstancePolicy, StateModel: policy.StateModel, Visibility: policy.Visibility,
			Routing: policy.Routing, RoutingDomain: policy.RoutingDomain, Replicas: service.Operations.Replicas,
		}
		if service.Operations.Autoscaling != nil {
			copy := *service.Operations.Autoscaling
			spec.Autoscaling = &copy
		}
		if service.Operations.Resources != nil {
			spec.Resources = *service.Operations.Resources
		}
		if service.Operations.Placement != nil {
			spec.Placement = *service.Operations.Placement
		}
		featureByRoot, selected := selectedFeatures(service, artifacts.manifests)
		for _, item := range selected {
			features = append(features, backendcompositionv1.ResolvedFeature{UnitID: service.ID, PluginID: item.pluginID, FeatureID: item.feature.ID})
		}
		items, err := configurationPlanForService(service, serviceCredentials, application, paths, featureByRoot, artifacts.manifests)
		if err != nil {
			return compileResult{}, fmt.Errorf("service %s: %w", service.ID, err)
		}
		configuration = append(configuration, items...)
		units = append(units, plannedUnit{
			service: service, application: application, installed: installed, paths: paths, featureByRoot: featureByRoot,
			unit: backendcompositionv1.ApplicationUnit{ServiceClass: service.ServiceClass, Spec: spec},
		})
	}
	bindings, graph, diagnostics, dependencies, err := planRuntime(intent, profile, units, artifacts.manifests)
	if err != nil {
		return compileResult{}, err
	}
	for index := range units {
		units[index].unit.Spec.DependsOn = append([]string(nil), dependencies[units[index].service.ID]...)
		sort.Strings(units[index].unit.Spec.DependsOn)
	}
	composition := backendcompositionv1.ApplicationComposition{
		Document: compositioncommonv1.Document{Version: 1, Revision: intent.Revision, ID: intent.ID},
		Target:   intent.Target, Metadata: intent.Metadata,
	}
	for _, unit := range units {
		composition.Units = append(composition.Units, unit.unit)
	}
	composition, err = backendcompositionv1.ValidateApplicationComposition(composition)
	if err != nil {
		return compileResult{}, fmt.Errorf("Planner 生成的 Application Composition 无效: %w", err)
	}
	return compileResult{composition: composition, features: features, bindings: bindings, graph: graph, configuration: configuration, diagnostics: diagnostics}, nil
}

func serviceClosure(service backendcompositionv1.ServiceIntent, artifacts resolvedArtifacts, locked map[string]pluginv1.ArtifactLockPackage) ([]string, map[string][]string, error) {
	queue := make([]string, 0, len(service.RootPlugins))
	paths := map[string][]string{}
	extra := map[string]map[string]string{}
	for _, root := range service.RootPlugins {
		queue = append(queue, root.PluginID)
		paths[root.PluginID] = []string{root.PluginID}
		if dependencies := artifacts.featureDeps[service.ID][root.PluginID]; len(dependencies) > 0 {
			extra[root.PluginID] = dependencies
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		_, ok := locked[id]
		if !ok {
			return nil, nil, fmt.Errorf("service %s 的依赖闭包缺少锁定插件 %s", service.ID, id)
		}
		manifest, exists := artifacts.manifests[id]
		if !exists {
			return nil, nil, fmt.Errorf("service %s 的依赖闭包缺少已验证 Manifest %s", service.ID, id)
		}
		dependencies := map[string]struct{}{}
		for dependency := range manifest.Dependencies {
			dependencies[dependency] = struct{}{}
		}
		for dependency := range extra[id] {
			dependencies[dependency] = struct{}{}
		}
		ids := make([]string, 0, len(dependencies))
		for dependency := range dependencies {
			ids = append(ids, dependency)
		}
		sort.Strings(ids)
		for _, dependency := range ids {
			if _, exists := paths[dependency]; exists {
				continue
			}
			paths[dependency] = append(append([]string(nil), paths[id]...), dependency)
			queue = append(queue, dependency)
		}
	}
	result := make([]string, 0, len(paths))
	for id := range paths {
		if _, baseline := artifacts.baselineIDs[id]; !baseline {
			result = append(result, id)
		}
	}
	sort.Strings(result)
	return result, paths, nil
}

func (s *Service) validateApplicationPlugins(ids []string, artifacts resolvedArtifacts) error {
	for _, id := range ids {
		descriptor, ok := artifacts.descriptors[id]
		if !ok {
			return fmt.Errorf("缺少插件 %s 的规划描述", id)
		}
		class, err := pluginid.ClassifyManagement(id, descriptor.Publisher)
		if err != nil {
			return err
		}
		if class == pluginid.ManagementPlatform {
			return fmt.Errorf("应用包依赖不能引入 Platform/Foundation 插件 %s；请由 Platform Profile 提供", id)
		}
		if class == pluginid.ManagementDevelopment && !s.config.AllowDevelopmentPlugins {
			return fmt.Errorf("生产规划策略不允许开发插件 %s", id)
		}
	}
	return nil
}

func applicationConfig(values map[string]map[string]any, credentials map[string]map[string]commonv1.ManagedCredentialRef, installed []string, manifests map[string]pluginv1.Manifest) (map[string]any, error) {
	allowed := map[string]struct{}{}
	for _, id := range installed {
		allowed[id] = struct{}{}
	}
	plugins := map[string]any{}
	for id, value := range values {
		if _, exists := allowed[id]; !exists {
			return nil, fmt.Errorf("pluginConfig 引用了当前服务依赖闭包之外的插件 %s", id)
		}
		plugins[id] = value
	}
	managed := map[string]any{}
	for id, fields := range credentials {
		if _, exists := allowed[id]; !exists {
			return nil, fmt.Errorf("Configuration Snapshot 引用了当前服务依赖闭包之外的插件 %s", id)
		}
		managed[id] = fields
	}
	kernelServiceGrants := map[string]any{}
	for _, id := range installed {
		manifest, exists := manifests[id]
		if !exists {
			return nil, fmt.Errorf("缺少插件 %s 的已验证 Manifest", id)
		}
		if manifest.Capabilities != nil && len(manifest.Capabilities.KernelServices) > 0 {
			kernelServiceGrants[id] = append([]string(nil), manifest.Capabilities.KernelServices...)
		}
	}
	if len(plugins) == 0 && len(managed) == 0 && len(kernelServiceGrants) == 0 {
		return nil, nil
	}
	result := map[string]any{}
	if len(plugins) > 0 {
		result["plugins"] = plugins
	}
	if len(managed) > 0 {
		result["managed_credentials"] = managed
	}
	if len(kernelServiceGrants) > 0 {
		result[pluginconfig.KernelServiceGrantsKey] = kernelServiceGrants
	}
	return result, nil
}

type selectedFeature struct {
	pluginID string
	feature  pluginv1.CompositionFeature
}

func selectedFeatures(service backendcompositionv1.ServiceIntent, manifests map[string]pluginv1.Manifest) (map[string][]pluginv1.CompositionFeature, []selectedFeature) {
	byRoot := map[string][]pluginv1.CompositionFeature{}
	var selected []selectedFeature
	for _, root := range service.RootPlugins {
		available := map[string]pluginv1.CompositionFeature{}
		if manifest := manifests[root.PluginID]; manifest.Composition != nil {
			for _, feature := range manifest.Composition.Features {
				available[feature.ID] = feature
			}
		}
		for _, id := range root.Features {
			feature := available[id]
			byRoot[root.PluginID] = append(byRoot[root.PluginID], feature)
			selected = append(selected, selectedFeature{pluginID: root.PluginID, feature: feature})
		}
	}
	return byRoot, selected
}
