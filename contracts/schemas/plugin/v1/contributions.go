package pluginv1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/servicemodel"
)

func BackendRuntimeContributions(manifest Manifest) ([]RuntimeContribution, error) {
	raw := manifest.Contributes["backend"]
	hasConfigurationController := manifest.Configuration != nil && manifest.Configuration.Controller != nil
	hasResourceController := manifest.Configuration != nil && manifest.Configuration.ResourceController != nil
	if len(raw) == 0 && !hasConfigurationController && !hasResourceController {
		return nil, nil
	}
	groups := map[string][]map[string]any{}
	if len(raw) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&groups); err != nil {
			return nil, fmt.Errorf("解析 backend contributions: %w", err)
		}
	}
	var out []RuntimeContribution
	defaultPolicy, overrides, err := runtimePolicies(manifest)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	for group, entries := range groups {
		point, ok := backendContributionPoints[group]
		if !ok {
			if _, declarative := declarativeBackendContributionGroups[group]; declarative {
				continue
			}
			return nil, fmt.Errorf("未知 backend contribution 组 %q", group)
		}
		for _, entry := range entries {
			id, _ := entry["id"].(string)
			if id == "" {
				return nil, fmt.Errorf("%s contribution 缺少 id", group)
			}
			priority := int32(0)
			if number, ok := entry["priority"].(json.Number); ok {
				parsed, err := number.Int64()
				if err != nil {
					return nil, fmt.Errorf("%s/%s priority 非整数: %w", point, id, err)
				}
				priority = int32(parsed)
			}
			delete(entry, "id")
			delete(entry, "priority")
			delete(entry, "service_role") // 装配归属由签名清单和 RuntimeUnit 单独强制。
			descriptor, err := json.Marshal(entry)
			if err != nil {
				return nil, fmt.Errorf("规范化 %s/%s descriptor: %w", point, id, err)
			}
			if err := ValidateDescriptor(point, descriptor); err != nil {
				return nil, err
			}
			policy := defaultPolicy
			if override, ok := overrides[point+"\x00"+id]; ok {
				policy.Visibility = override.Visibility
				policy.Routing = override.Routing
				policy.RoutingDomain = override.RoutingDomain
				policy = servicemodel.Normalize(policy)
			}
			key := point + "\x00" + id
			if _, duplicate := seen[key]; duplicate {
				return nil, fmt.Errorf("运行时贡献重复: %s/%s", point, id)
			}
			contract, declared := overrides[key]
			if !declared {
				return nil, fmt.Errorf("运行时贡献 %s/%s 缺少 runtime.provides 契约声明", point, id)
			}
			seen[key] = struct{}{}
			out = append(out, RuntimeContribution{
				ExtensionPoint: point, ID: id, ContractVersion: contract.ContractVersion,
				Priority: priority, Descriptor: descriptor,
				InstancePolicy: policy.InstancePolicy, StateModel: policy.StateModel,
				Visibility: policy.Visibility, Routing: policy.Routing, RoutingDomain: policy.RoutingDomain,
			})
		}
	}
	if hasConfigurationController {
		id, err := ConfigurationControllerCapability(manifest.ID)
		if err != nil {
			return nil, err
		}
		descriptor := configurationControllerDescriptor()
		if err := ValidateDescriptor(ConfigurationControllerExtensionPoint, descriptor); err != nil {
			return nil, err
		}
		key := ConfigurationControllerExtensionPoint + "\x00" + id
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("运行时贡献重复: %s/%s", ConfigurationControllerExtensionPoint, id)
		}
		seen[key] = struct{}{}
		policy := defaultPolicy
		if override, ok := overrides[key]; ok {
			policy.Visibility = override.Visibility
			policy.Routing = override.Routing
			policy.RoutingDomain = override.RoutingDomain
			policy = servicemodel.Normalize(policy)
		}
		out = append(out, RuntimeContribution{
			ExtensionPoint: ConfigurationControllerExtensionPoint, ID: id,
			ContractVersion: "1.0.0", Descriptor: descriptor,
			InstancePolicy: policy.InstancePolicy, StateModel: policy.StateModel,
			Visibility: policy.Visibility, Routing: policy.Routing, RoutingDomain: policy.RoutingDomain,
		})
	}
	if hasResourceController {
		id, err := ConfigurationResourceControllerCapability(manifest.ID)
		if err != nil {
			return nil, err
		}
		descriptor := configurationResourceControllerDescriptor()
		if err := ValidateDescriptor(ConfigurationResourceControllerExtensionPoint, descriptor); err != nil {
			return nil, err
		}
		key := ConfigurationResourceControllerExtensionPoint + "\x00" + id
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("运行时贡献重复: %s/%s", ConfigurationResourceControllerExtensionPoint, id)
		}
		seen[key] = struct{}{}
		policy := defaultPolicy
		if override, ok := overrides[key]; ok {
			policy.Visibility = override.Visibility
			policy.Routing = override.Routing
			policy.RoutingDomain = override.RoutingDomain
			policy = servicemodel.Normalize(policy)
		}
		out = append(out, RuntimeContribution{
			ExtensionPoint: ConfigurationResourceControllerExtensionPoint, ID: id,
			ContractVersion: "1.0.0", Descriptor: descriptor,
			InstancePolicy: policy.InstancePolicy, StateModel: policy.StateModel,
			Visibility: policy.Visibility, Routing: policy.Routing, RoutingDomain: policy.RoutingDomain,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ExtensionPoint != out[j].ExtensionPoint {
			return out[i].ExtensionPoint < out[j].ExtensionPoint
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// ManifestDataModels returns the signed references to external DataModel files.
func ManifestDataModels(manifest Manifest) ([]DataModelReference, error) {
	var backend struct {
		DataModels []DataModelReference `json:"dataModels"`
	}
	if raw := manifest.Contributes["backend"]; len(raw) != 0 {
		if err := json.Unmarshal(raw, &backend); err != nil {
			return nil, fmt.Errorf("解析 backend.dataModels: %w", err)
		}
	}
	seen := map[string]struct{}{}
	for _, reference := range backend.DataModels {
		if _, duplicate := seen[reference.ID]; duplicate {
			return nil, fmt.Errorf("backend.dataModels 重复: %s", reference.ID)
		}
		seen[reference.ID] = struct{}{}
	}
	return append([]DataModelReference(nil), backend.DataModels...), nil
}

// ManifestDataMigrations returns immutable migration documents carried by the
// verified plugin artifact. IDs and model/version edges are unique per plugin.
func ManifestDataMigrations(manifest Manifest) ([]DataMigrationReference, error) {
	var backend struct {
		DataMigrations []DataMigrationReference `json:"dataMigrations"`
	}
	if raw := manifest.Contributes["backend"]; len(raw) != 0 {
		if err := json.Unmarshal(raw, &backend); err != nil {
			return nil, fmt.Errorf("解析 backend.dataMigrations: %w", err)
		}
	}
	models, err := ManifestDataModels(manifest)
	if err != nil {
		return nil, err
	}
	modelIDs := map[string]struct{}{}
	for _, model := range models {
		modelIDs[model.ID] = struct{}{}
	}
	ids, edges := map[string]struct{}{}, map[string]struct{}{}
	for _, reference := range backend.DataMigrations {
		if _, declared := modelIDs[reference.ModelID]; !declared || reference.FromVersion >= reference.ToVersion {
			return nil, fmt.Errorf("backend.dataMigrations 未绑定有效 DataModel 版本边: %s", reference.ID)
		}
		if _, duplicate := ids[reference.ID]; duplicate {
			return nil, fmt.Errorf("backend.dataMigrations ID 重复: %s", reference.ID)
		}
		ids[reference.ID] = struct{}{}
		edge := fmt.Sprintf("%s:%d:%d", reference.ModelID, reference.FromVersion, reference.ToVersion)
		if _, duplicate := edges[edge]; duplicate {
			return nil, fmt.Errorf("backend.dataMigrations 版本边重复: %s", edge)
		}
		edges[edge] = struct{}{}
	}
	return append([]DataMigrationReference(nil), backend.DataMigrations...), nil
}

// IsLocalPermissionAuxiliary reports whether a contribution is a host-local
// authorization guard that may be co-located with a service unit whose
// schedulable capability uses a cluster policy. The exception is intentionally
// narrow: arbitrary local tools must remain separate units and cannot use this
// predicate to escape deployment-policy validation.
func IsLocalPermissionAuxiliary(contribution RuntimeContribution) bool {
	return contribution.ExtensionPoint == "permission.checker" &&
		contribution.InstancePolicy == "per-kernel" &&
		contribution.StateModel == "local-ephemeral" &&
		contribution.Visibility == "local" &&
		contribution.Routing == "direct" &&
		contribution.RoutingDomain == ""
}
