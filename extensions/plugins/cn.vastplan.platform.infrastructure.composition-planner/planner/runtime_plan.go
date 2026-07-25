package planner

import (
	"fmt"
	"sort"

	semver "github.com/Masterminds/semver/v3"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/servicemodel"
)

type runtimeProvider struct {
	unitID, pluginID, capability, version string
	logicalService, routingDomain         string
	visibility                            string
}

func policyForUnit(pluginIDs []string, manifests map[string]pluginv1.Manifest) (servicemodel.Policy, error) {
	var selected *servicemodel.Policy
	for _, id := range pluginIDs {
		manifest, ok := manifests[id]
		if !ok {
			return servicemodel.Policy{}, fmt.Errorf("缺少插件 %s 的运行策略描述", id)
		}
		contributions, err := pluginv1.BackendRuntimeContributions(manifest)
		if err != nil {
			return servicemodel.Policy{}, err
		}
		found := false
		for _, contribution := range contributions {
			if pluginv1.IsLocalPermissionAuxiliary(contribution) {
				continue
			}
			found = true
			policy := servicemodel.Normalize(servicemodel.Policy{
				InstancePolicy: contribution.InstancePolicy, StateModel: contribution.StateModel,
				Visibility: contribution.Visibility, Routing: contribution.Routing, RoutingDomain: contribution.RoutingDomain,
			})
			if selected != nil && !servicemodel.Equal(*selected, policy) {
				return servicemodel.Policy{}, fmt.Errorf("插件 %s 的运行策略与同一 service 中的其他非辅助贡献不兼容", id)
			}
			copy := policy
			selected = &copy
		}
		if !found && manifest.Runtime != nil && manifest.Runtime.BackgroundService {
			policy := servicemodel.Normalize(servicemodel.Policy{
				InstancePolicy: manifest.Runtime.InstancePolicy, StateModel: manifest.Runtime.StateModel,
				Visibility: manifest.Runtime.Visibility, Routing: manifest.Runtime.Routing, RoutingDomain: manifest.Runtime.RoutingDomain,
			})
			if selected != nil && !servicemodel.Equal(*selected, policy) {
				return servicemodel.Policy{}, fmt.Errorf("后台插件 %s 的运行策略与同一 service 不兼容", id)
			}
			copy := policy
			selected = &copy
		}
	}
	if selected == nil {
		policy := servicemodel.Normalize(servicemodel.Policy{})
		selected = &policy
	}
	if err := servicemodel.Validate(*selected); err != nil {
		return servicemodel.Policy{}, err
	}
	return *selected, nil
}

func planRuntime(intent backendcompositionv1.ApplicationIntent, profile backendcompositionv1.PlatformProfile, units []plannedUnit, manifests map[string]pluginv1.Manifest) ([]backendcompositionv1.CapabilityProviderBinding, backendcompositionv1.ServiceDependencyGraph, []backendcompositionv1.ResolutionDiagnostic, map[string][]string, error) {
	graph := backendcompositionv1.ServiceDependencyGraph{Nodes: []backendcompositionv1.ServiceDependencyNode{}, Edges: []backendcompositionv1.ServiceDependencyEdge{}}
	providers := map[string][]runtimeProvider{}
	applicationUnits := map[string]struct{}{}
	for _, planned := range units {
		id := planned.unit.Spec.ID
		if _, duplicate := applicationUnits[id]; duplicate {
			return nil, graph, nil, nil, fmt.Errorf("Application Intent service id 重复: %s", id)
		}
		applicationUnits[id] = struct{}{}
		graph.Nodes = append(graph.Nodes, backendcompositionv1.ServiceDependencyNode{UnitID: id, ServiceClass: planned.unit.ServiceClass})
		collectProviders(providers, id, planned.unit.Spec.LogicalService, planned.installed, manifests)
	}
	for _, service := range profile.Services {
		if _, collision := applicationUnits[service.ID]; collision {
			return nil, graph, nil, nil, fmt.Errorf("应用 service %s 与 Platform Profile service 冲突", service.ID)
		}
		graph.Nodes = append(graph.Nodes, backendcompositionv1.ServiceDependencyNode{UnitID: service.ID, ServiceClass: "platform.backend"})
		ids := make([]string, 0, len(service.Plugins))
		for _, ref := range service.Plugins {
			ids = append(ids, ref.ID)
		}
		collectProviders(providers, service.ID, service.LogicalService, ids, manifests)
	}
	bindingsByKey := map[string]backendcompositionv1.CapabilityProviderBinding{}
	edgesByKey := map[string]backendcompositionv1.ServiceDependencyEdge{}
	dependencies := map[string][]string{}
	var diagnostics []backendcompositionv1.ResolutionDiagnostic
	for _, planned := range units {
		requirements := requirementsForUnit(planned, manifests)
		for _, requirement := range requirements {
			matches, err := matchRuntimeProviders(requirement, providers[requirement.Capability], planned.service.ID)
			if err != nil {
				return nil, graph, diagnostics, nil, fmt.Errorf("service %s capability %s: %w", planned.service.ID, requirement.Capability, err)
			}
			if len(matches) == 0 {
				if requirement.Kind == "strong" || requirement.Kind == "data" {
					return nil, graph, diagnostics, nil, fmt.Errorf("service %s 的阻塞 capability %s 没有唯一可用 Provider", planned.service.ID, requirement.Capability)
				}
				diagnostics = append(diagnostics, backendcompositionv1.ResolutionDiagnostic{
					Severity: "warning", Code: "composition.provider.optional-missing",
					Path: []string{planned.service.ID, requirement.Capability}, Message: "可选运行时能力当前没有可用 Provider",
				})
				continue
			}
			provider := matches[0]
			binding := backendcompositionv1.CapabilityProviderBinding{
				ConsumerUnitID: planned.service.ID, Capability: requirement.Capability,
				ProviderUnitID: provider.unitID, ProviderPluginID: provider.pluginID, Version: provider.version,
				LogicalService: provider.logicalService, RoutingDomain: provider.routingDomain,
			}
			key := planned.service.ID + "\x00" + requirement.Capability
			if existing, duplicate := bindingsByKey[key]; duplicate && existing != binding {
				return nil, graph, diagnostics, nil, fmt.Errorf("service %s 对 capability %s 的多个要求解析到不同 Provider", planned.service.ID, requirement.Capability)
			}
			bindingsByKey[key] = binding
			if (requirement.Kind == "strong" || requirement.Kind == "data") && provider.unitID != planned.service.ID {
				edge := backendcompositionv1.ServiceDependencyEdge{
					FromUnitID: planned.service.ID, ToUnitID: provider.unitID, Capability: requirement.Capability,
					Kind: requirement.Kind, FailurePolicy: requirement.FailurePolicy,
				}
				edgesByKey[edge.FromUnitID+"\x00"+edge.ToUnitID+"\x00"+edge.Capability] = edge
				if _, applicationProvider := applicationUnits[provider.unitID]; applicationProvider {
					dependencies[planned.service.ID] = appendUnique(dependencies[planned.service.ID], provider.unitID)
				}
			}
		}
	}
	bindings := make([]backendcompositionv1.CapabilityProviderBinding, 0, len(bindingsByKey))
	for _, binding := range bindingsByKey {
		bindings = append(bindings, binding)
	}
	for _, edge := range edgesByKey {
		graph.Edges = append(graph.Edges, edge)
	}
	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].ConsumerUnitID != bindings[j].ConsumerUnitID {
			return bindings[i].ConsumerUnitID < bindings[j].ConsumerUnitID
		}
		return bindings[i].Capability < bindings[j].Capability
	})
	return bindings, graph, diagnostics, dependencies, nil
}

func collectProviders(target map[string][]runtimeProvider, unitID, logicalService string, pluginIDs []string, manifests map[string]pluginv1.Manifest) {
	for _, pluginID := range pluginIDs {
		manifest, ok := manifests[pluginID]
		if !ok {
			continue
		}
		contributions, _ := pluginv1.BackendRuntimeContributions(manifest)
		for _, contribution := range contributions {
			target[contribution.ID] = append(target[contribution.ID], runtimeProvider{
				unitID: unitID, pluginID: pluginID, capability: contribution.ID, version: manifest.Version,
				logicalService: logicalService, routingDomain: contribution.RoutingDomain, visibility: contribution.Visibility,
			})
		}
	}
}

func requirementsForUnit(unit plannedUnit, manifests map[string]pluginv1.Manifest) []pluginv1.RuntimeRequirement {
	var result []pluginv1.RuntimeRequirement
	for _, id := range unit.installed {
		if manifest := manifests[id]; manifest.Runtime != nil {
			result = append(result, manifest.Runtime.Requires...)
		}
	}
	for _, features := range unit.featureByRoot {
		for _, feature := range features {
			result = append(result, feature.RuntimeRequires...)
		}
	}
	return result
}

func matchRuntimeProviders(requirement pluginv1.RuntimeRequirement, candidates []runtimeProvider, consumerUnitID string) ([]runtimeProvider, error) {
	var constraint *semver.Constraints
	if requirement.Version != "" {
		var err error
		constraint, err = semver.NewConstraint(requirement.Version)
		if err != nil {
			return nil, fmt.Errorf("版本范围无效: %w", err)
		}
	}
	var result []runtimeProvider
	for _, candidate := range candidates {
		if (requirement.Scope == "same-node" || requirement.Scope == "same-kernel") && candidate.unitID != consumerUnitID {
			continue
		}
		if requirement.Scope == "remote" && candidate.visibility == servicemodel.VisibilityLocal {
			continue
		}
		if requirement.LogicalService != "" && candidate.logicalService != requirement.LogicalService {
			continue
		}
		if requirement.RoutingDomain != "" && candidate.routingDomain != requirement.RoutingDomain {
			continue
		}
		if constraint != nil {
			version, err := semver.NewVersion(candidate.version)
			if err != nil || !constraint.Check(version) {
				continue
			}
		}
		result = append(result, candidate)
	}
	if len(result) > 1 {
		return nil, fmt.Errorf("存在 %d 个候选 Provider，必须由签名 logicalService/routingDomain 消除歧义", len(result))
	}
	return result, nil
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}
