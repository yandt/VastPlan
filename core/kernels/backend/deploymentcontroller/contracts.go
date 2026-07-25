package deploymentcontroller

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginid"
	"cdsoft.com.cn/VastPlan/core/shared/go/servicemodel"
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
	contracts, providers, versionsByPlugin, err := collectDeploymentContracts(deployment, artifacts)
	if err != nil {
		return err
	}
	if err := validatePluginVersions(versionsByPlugin); err != nil {
		return err
	}
	for _, contract := range contracts {
		if err := validateUnitRequirements(contract, providers, versionsByPlugin, graph); err != nil {
			return err
		}
	}
	return nil
}

func collectDeploymentContracts(deployment deploymentv2.Deployment, artifacts ArtifactReader) (map[string]unitContract, map[string][]capabilityProvider, map[string]map[string]struct{}, error) {
	contracts := make(map[string]unitContract, len(deployment.Units))
	providers := map[string][]capabilityProvider{}
	versionsByPlugin := map[string]map[string]struct{}{}
	for _, unit := range deployment.Units {
		contract, err := collectUnitContract(deployment, unit, artifacts, providers, versionsByPlugin)
		if err != nil {
			return nil, nil, nil, err
		}
		contracts[unit.ID] = contract
	}
	return contracts, providers, versionsByPlugin, nil
}

func collectUnitContract(deployment deploymentv2.Deployment, unit deploymentv2.ServiceUnit, artifacts ArtifactReader, providers map[string][]capabilityProvider, versionsByPlugin map[string]map[string]struct{}) (unitContract, error) {
	contract := unitContract{unit: unit}
	for _, ref := range unit.Plugins {
		manifest, err := loadContractManifest(deployment, unit, ref, artifacts)
		if err != nil {
			return unitContract{}, err
		}
		if err := recordCapabilityProviders(unit, ref, manifest, providers); err != nil {
			return unitContract{}, err
		}
		contract.manifests = append(contract.manifests, manifest)
		if versionsByPlugin[ref.ID] == nil {
			versionsByPlugin[ref.ID] = map[string]struct{}{}
		}
		versionsByPlugin[ref.ID][ref.Version] = struct{}{}
	}
	return contract, nil
}

func loadContractManifest(deployment deploymentv2.Deployment, unit deploymentv2.ServiceUnit, ref deploymentv1.PluginRef, artifacts ArtifactReader) (pluginv1.Manifest, error) {
	origin, ok := deployment.Resolution.PluginOrigins[ref.ID]
	if !ok {
		return pluginv1.Manifest{}, fmt.Errorf("unit %s 插件 %s 缺少解析来源", unit.ID, ref.ID)
	}
	if origin != deploymentv2.OriginPlatformProfile && origin != deploymentv2.OriginApplication {
		return pluginv1.Manifest{}, fmt.Errorf("unit %s 插件 %s 的解析来源无效: %q", unit.ID, ref.ID, origin)
	}
	artifactRef := pluginv1.ArtifactRef{PluginID: ref.ID, Version: ref.Version, Channel: normalizedChannel(ref)}
	artifact, _, err := artifacts.Read(artifactRef)
	if err != nil {
		return pluginv1.Manifest{}, fmt.Errorf("unit %s 读取制品 %s@%s: %w", unit.ID, ref.ID, ref.Version, err)
	}
	manifest, err := pluginv1.ParseManifest(artifact.Manifest)
	if err != nil {
		return pluginv1.Manifest{}, fmt.Errorf("unit %s 制品 %s 清单无效: %w", unit.ID, ref.ID, err)
	}
	if artifact.PluginID != ref.ID || artifact.Version != ref.Version || normalizeChannel(artifact.Channel) != artifactRef.Channel || manifest.ID != ref.ID || manifest.Version != ref.Version {
		return pluginv1.Manifest{}, fmt.Errorf("unit %s 制品引用与不可变清单身份不一致: %s@%s", unit.ID, ref.ID, ref.Version)
	}
	class, err := pluginid.ClassifyManagement(manifest.ID, manifest.Publisher)
	if err != nil {
		return pluginv1.Manifest{}, fmt.Errorf("unit %s 插件 %s 身份分类失败: %w", unit.ID, ref.ID, err)
	}
	if class == pluginid.ManagementDevelopment && !deployment.Resolution.DevelopmentMode {
		return pluginv1.Manifest{}, fmt.Errorf("unit %s 包含未允许的开发插件 %s", unit.ID, ref.ID)
	}
	if origin == deploymentv2.OriginApplication && class == pluginid.ManagementPlatform {
		return pluginv1.Manifest{}, fmt.Errorf("unit %s 的应用来源不能包含平台管理插件 %s", unit.ID, ref.ID)
	}
	return manifest, nil
}

func recordCapabilityProviders(unit deploymentv2.ServiceUnit, ref deploymentv1.PluginRef, manifest pluginv1.Manifest, providers map[string][]capabilityProvider) error {
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil {
		return fmt.Errorf("unit %s 解析 %s runtime: %w", unit.ID, ref.ID, err)
	}
	unitPolicy := servicemodel.Policy{
		InstancePolicy: unit.InstancePolicy, StateModel: unit.StateModel,
		Visibility: unit.Visibility, Routing: unit.Routing, RoutingDomain: unit.RoutingDomain,
	}
	for _, contribution := range contributions {
		contributionPolicy := servicemodel.Policy{
			InstancePolicy: contribution.InstancePolicy, StateModel: contribution.StateModel,
			Visibility: contribution.Visibility, Routing: contribution.Routing, RoutingDomain: contribution.RoutingDomain,
		}
		if !servicemodel.Equal(contributionPolicy, unitPolicy) && !pluginv1.IsLocalPermissionAuxiliary(contribution) {
			return fmt.Errorf("unit %s 部署策略与签名清单 %s/%s 不一致", unit.ID, ref.ID, contribution.ID)
		}
		providers[contribution.ID] = append(providers[contribution.ID], capabilityProvider{
			unitID: unit.ID, capability: contribution.ID, version: manifest.Version,
			logicalService: unit.LogicalService, routingDomain: contribution.RoutingDomain, visibility: contribution.Visibility,
		})
	}
	return nil
}

func validatePluginVersions(versionsByPlugin map[string]map[string]struct{}) error {
	for pluginID, versions := range versionsByPlugin {
		if len(versions) > 1 {
			return fmt.Errorf("部署中插件 %s 存在不可判定的多版本冲突: %s", pluginID, strings.Join(sortedSet(versions), ", "))
		}
	}
	return nil
}

func validateUnitRequirements(contract unitContract, providers map[string][]capabilityProvider, versionsByPlugin map[string]map[string]struct{}, graph map[string][]string) error {
	for _, manifest := range contract.manifests {
		if err := validatePackageDependencies(contract.unit.ID, manifest, versionsByPlugin); err != nil {
			return err
		}
		if manifest.Runtime == nil {
			continue
		}
		for _, requirement := range manifest.Runtime.Requires {
			if err := validateRuntimeRequirement(contract.unit.ID, requirement, providers[requirement.Capability], graph); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateRuntimeRequirement(unitID string, requirement pluginv1.RuntimeRequirement, available []capabilityProvider, graph map[string][]string) error {
	matches, mismatch := matchingProviders(requirement, available)
	switch requirement.Scope {
	case "same-node", "same-kernel":
		matches = providersInUnit(matches, unitID)
		if len(matches) == 0 && strongRequirement(requirement) {
			return fmt.Errorf("unit %s 的 %s 依赖 %s 必须由同一 service unit 提供", unitID, requirement.Scope, requirement.Capability)
		}
	case "remote":
		if err := validateRemoteRequirement(unitID, requirement, matches, mismatch); err != nil {
			return err
		}
		for _, provider := range matches {
			if provider.visibility == servicemodel.VisibilityLocal {
				return fmt.Errorf("unit %s 不能远端依赖 local capability %s", unitID, requirement.Capability)
			}
			if strongRequirement(requirement) && provider.unitID != unitID {
				graph[unitID] = appendUnique(graph[unitID], provider.unitID)
			}
		}
	}
	return nil
}

func validateRemoteRequirement(unitID string, requirement pluginv1.RuntimeRequirement, matches []capabilityProvider, mismatch bool) error {
	if len(matches) != 0 || !strongRequirement(requirement) {
		return nil
	}
	if mismatch {
		return fmt.Errorf("unit %s 的 capability %s 版本范围 %q 无可用提供者", unitID, requirement.Capability, requirement.Version)
	}
	// A deployment revision only owns its own units. An explicitly addressed
	// remote service may belong to another deployment. Availability and version
	// are fenced by the global readiness directory at the Node Agent.
	if requirement.LogicalService == "" || requirement.RoutingDomain == "" {
		return fmt.Errorf("unit %s 缺少远端 capability %s", unitID, requirement.Capability)
	}
	return nil
}

func strongRequirement(requirement pluginv1.RuntimeRequirement) bool {
	return requirement.Kind == "strong" || requirement.Kind == "data"
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
	return normalizeChannel(ref.Channel)
}

func normalizeChannel(channel string) string {
	if channel == "" {
		return "stable"
	}
	return channel
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
