package nodeagent

import (
	"fmt"
	"sort"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/servicemodel"
)

func partitionKeys(config map[string]any) []string {
	return normalizedStringList(config, pluginconfig.PartitionKeysKey)
}

func configEnvelope(config map[string]any, plugins []deploymentv1.PluginRef) (pluginconfig.Envelope, error) {
	ids := make([]string, 0, len(plugins))
	for _, plugin := range plugins {
		ids = append(ids, plugin.ID)
	}
	return pluginconfig.Parse(config, ids)
}

func normalizedStringList(config map[string]any, key string) []string {
	raw, ok := config[key]
	if !ok {
		return nil
	}
	var keys []string
	switch values := raw.(type) {
	case []string:
		keys = append(keys, values...)
	case []any:
		for _, value := range values {
			if key, ok := value.(string); ok && key != "" {
				keys = append(keys, key)
			}
		}
	}
	sort.Strings(keys)
	seen := make(map[string]struct{}, len(keys))
	unique := keys[:0]
	for _, key := range keys {
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, key)
	}
	return unique
}

func unitPolicy(unit deploymentv1.Unit) (servicemodel.Policy, error) {
	policy := servicemodel.Normalize(servicemodel.Policy{
		InstancePolicy: unit.InstancePolicy,
		StateModel:     unit.StateModel,
		Visibility:     unit.Visibility,
		Routing:        unit.Routing,
		RoutingDomain:  unit.RoutingDomain,
	})
	if err := servicemodel.Validate(policy); err != nil {
		return servicemodel.Policy{}, fmt.Errorf("unit %s 运行策略无效: %w", unit.ID, err)
	}
	return policy, nil
}

func contributionPolicy(contribution pluginv1.RuntimeContribution) (servicemodel.Policy, error) {
	policy := servicemodel.Policy{
		InstancePolicy: contribution.InstancePolicy,
		StateModel:     contribution.StateModel,
		Visibility:     contribution.Visibility,
		Routing:        contribution.Routing,
		RoutingDomain:  contribution.RoutingDomain,
	}
	policy = servicemodel.Normalize(policy)
	if err := servicemodel.Validate(policy); err != nil {
		return servicemodel.Policy{}, fmt.Errorf("贡献 %s/%s 运行策略无效: %w", contribution.ExtensionPoint, contribution.ID, err)
	}
	return policy, nil
}

func validateInstalledPolicies(unit servicemodel.Policy, plugins []InstalledPlugin) error {
	for _, plugin := range plugins {
		for _, contribution := range plugin.Contract.Contributions {
			policy, err := contributionPolicy(contribution)
			if err != nil {
				return fmt.Errorf("插件 %s: %w", plugin.ID, err)
			}
			if !servicemodel.Equal(unit, policy) && !pluginv1.IsLocalPermissionAuxiliary(contribution) {
				return fmt.Errorf("插件 %s 的贡献 %s/%s 运行策略与部署不一致: manifest=%+v deployment=%+v",
					plugin.ID, contribution.ExtensionPoint, contribution.ID, policy, unit)
			}
		}
	}
	return nil
}
