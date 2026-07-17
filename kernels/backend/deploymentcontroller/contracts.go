package deploymentcontroller

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"

	deploymentv1 "cdsoft.com.cn/VastPlan/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/shared/go/servicemodel"
)

// ArtifactReader 是控制面需要的最小制品视图。生产入口注入与 Node Agent 相同的
// 不可变仓库；接口留在控制器包，避免调度层反向依赖插件服务实现。
type ArtifactReader interface {
	Read(pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, error)
}

type capabilityProvider struct {
	unitID         string
	capability     string
	version        string
	logicalService string
	routingDomain  string
	visibility     string
}

type unitContract struct {
	unit      deploymentv2.ServiceUnit
	manifests []pluginv1.Manifest
}

func validateDeploymentContracts(deployment deploymentv2.Deployment, graph map[string][]string, artifacts ArtifactReader) error {
	contracts := make(map[string]unitContract, len(deployment.Units))
	providers := map[string][]capabilityProvider{}
	versionsByPlugin := map[string]map[string]struct{}{}
	for _, unit := range deployment.Units {
		contract := unitContract{unit: unit}
		for _, ref := range unit.Plugins {
			artifactRef := pluginv1.ArtifactRef{PluginID: ref.ID, Version: ref.Version, Channel: normalizedChannel(ref)}
			artifact, _, err := artifacts.Read(artifactRef)
			if err != nil {
				return fmt.Errorf("unit %s 读取制品 %s@%s: %w", unit.ID, ref.ID, ref.Version, err)
			}
			manifest, err := pluginv1.ParseManifest(artifact.Manifest)
			if err != nil {
				return fmt.Errorf("unit %s 制品 %s 清单无效: %w", unit.ID, ref.ID, err)
			}
			if artifact.PluginID != ref.ID || artifact.Version != ref.Version || manifest.ID != ref.ID || manifest.Version != ref.Version {
				return fmt.Errorf("unit %s 制品引用与不可变清单身份不一致: %s@%s", unit.ID, ref.ID, ref.Version)
			}
			contributions, err := pluginv1.BackendRuntimeContributions(manifest)
			if err != nil {
				return fmt.Errorf("unit %s 解析 %s runtime: %w", unit.ID, ref.ID, err)
			}
			for _, contribution := range contributions {
				contributionPolicy := servicemodel.Policy{
					InstancePolicy: contribution.InstancePolicy, StateModel: contribution.StateModel,
					Visibility: contribution.Visibility, Routing: contribution.Routing, RoutingDomain: contribution.RoutingDomain,
				}
				unitPolicy := servicemodel.Policy{
					InstancePolicy: unit.InstancePolicy, StateModel: unit.StateModel,
					Visibility: unit.Visibility, Routing: unit.Routing, RoutingDomain: unit.RoutingDomain,
				}
				if !servicemodel.Equal(contributionPolicy, unitPolicy) {
					return fmt.Errorf("unit %s 部署策略与签名清单 %s/%s 不一致", unit.ID, ref.ID, contribution.ID)
				}
				providers[contribution.ID] = append(providers[contribution.ID], capabilityProvider{
					unitID: unit.ID, capability: contribution.ID, version: manifest.Version,
					logicalService: unit.LogicalService, routingDomain: contribution.RoutingDomain, visibility: contribution.Visibility,
				})
			}
			contract.manifests = append(contract.manifests, manifest)
			if versionsByPlugin[ref.ID] == nil {
				versionsByPlugin[ref.ID] = map[string]struct{}{}
			}
			versionsByPlugin[ref.ID][ref.Version] = struct{}{}
		}
		contracts[unit.ID] = contract
	}

	for pluginID, versions := range versionsByPlugin {
		if len(versions) > 1 {
			return fmt.Errorf("部署中插件 %s 存在不可判定的多版本冲突: %s", pluginID, strings.Join(sortedSet(versions), ", "))
		}
	}
	for _, contract := range contracts {
		for _, manifest := range contract.manifests {
			if err := validatePackageDependencies(contract.unit.ID, manifest, versionsByPlugin); err != nil {
				return err
			}
			if manifest.Runtime == nil {
				continue
			}
			for _, requirement := range manifest.Runtime.Requires {
				matches, mismatch := matchingProviders(requirement, providers[requirement.Capability])
				switch requirement.Scope {
				case "same-node", "same-kernel":
					matches = providersInUnit(matches, contract.unit.ID)
					if len(matches) == 0 && (requirement.Kind == "strong" || requirement.Kind == "data") {
						return fmt.Errorf("unit %s 的 %s 依赖 %s 必须由同一 service unit 提供", contract.unit.ID, requirement.Scope, requirement.Capability)
					}
				case "remote":
					if len(matches) == 0 && (requirement.Kind == "strong" || requirement.Kind == "data") {
						if mismatch {
							return fmt.Errorf("unit %s 的 capability %s 版本范围 %q 无可用提供者", contract.unit.ID, requirement.Capability, requirement.Version)
						}
						return fmt.Errorf("unit %s 缺少远端 capability %s", contract.unit.ID, requirement.Capability)
					}
					for _, provider := range matches {
						if provider.visibility == servicemodel.VisibilityLocal {
							return fmt.Errorf("unit %s 不能远端依赖 local capability %s", contract.unit.ID, requirement.Capability)
						}
						if (requirement.Kind == "strong" || requirement.Kind == "data") && provider.unitID != contract.unit.ID {
							graph[contract.unit.ID] = appendUnique(graph[contract.unit.ID], provider.unitID)
						}
					}
				}
			}
		}
	}
	return nil
}

func validatePackageDependencies(unitID string, manifest pluginv1.Manifest, versions map[string]map[string]struct{}) error {
	for pluginID, constraintText := range manifest.Dependencies {
		deployed := versions[pluginID]
		if len(deployed) == 0 {
			return fmt.Errorf("unit %s 的制品 %s 缺少包依赖 %s", unitID, manifest.ID, pluginID)
		}
		constraint, err := semver.NewConstraint(constraintText)
		if err != nil {
			return fmt.Errorf("制品 %s 的依赖范围 %s=%q 无效: %w", manifest.ID, pluginID, constraintText, err)
		}
		matched := false
		for version := range deployed {
			parsed, parseErr := semver.NewVersion(version)
			if parseErr == nil && constraint.Check(parsed) {
				matched = true
			}
		}
		if !matched {
			return fmt.Errorf("unit %s 的制品 %s 需要 %s %s，部署版本为 %s", unitID, manifest.ID, pluginID, constraintText, strings.Join(sortedSet(deployed), ", "))
		}
	}
	return nil
}

func matchingProviders(requirement pluginv1.RuntimeRequirement, candidates []capabilityProvider) ([]capabilityProvider, bool) {
	var constraint *semver.Constraints
	if requirement.Version != "" {
		constraint, _ = semver.NewConstraint(requirement.Version)
	}
	mismatch := false
	matches := make([]capabilityProvider, 0, len(candidates))
	for _, candidate := range candidates {
		if requirement.LogicalService != "" && candidate.logicalService != requirement.LogicalService {
			continue
		}
		if requirement.RoutingDomain != "" && candidate.routingDomain != requirement.RoutingDomain {
			continue
		}
		if constraint != nil {
			version, err := semver.NewVersion(candidate.version)
			if err != nil || !constraint.Check(version) {
				mismatch = true
				continue
			}
		}
		matches = append(matches, candidate)
	}
	return matches, mismatch
}

func providersInUnit(providers []capabilityProvider, unitID string) []capabilityProvider {
	filtered := providers[:0]
	for _, provider := range providers {
		if provider.unitID == unitID {
			filtered = append(filtered, provider)
		}
	}
	return filtered
}

func normalizedChannel(ref deploymentv1.PluginRef) string {
	if ref.Channel == "" {
		return "stable"
	}
	return ref.Channel
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func sortedSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
