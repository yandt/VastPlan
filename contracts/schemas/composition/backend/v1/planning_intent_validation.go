package backendcompositionv1

import (
	"encoding/json"
	"fmt"
	"sort"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

func ParseApplicationIntent(raw []byte) (ApplicationIntent, error) {
	schema, _, err := planningSchemas()
	if err != nil {
		return ApplicationIntent{}, err
	}
	if err := validateJSON(schema, raw, "Backend Application Intent"); err != nil {
		return ApplicationIntent{}, err
	}
	var intent ApplicationIntent
	if err := json.Unmarshal(raw, &intent); err != nil {
		return ApplicationIntent{}, fmt.Errorf("解析 Backend Application Intent 字段: %w", err)
	}
	return NormalizeApplicationIntent(intent)
}

func ValidateApplicationIntent(intent ApplicationIntent) (ApplicationIntent, error) {
	raw, err := json.Marshal(intent)
	if err != nil {
		return ApplicationIntent{}, fmt.Errorf("编码 Backend Application Intent: %w", err)
	}
	return ParseApplicationIntent(raw)
}

func NormalizeApplicationIntent(intent ApplicationIntent) (ApplicationIntent, error) {
	if err := compositioncommonv1.ValidateTarget(intent.Target, compositioncommonv1.KernelBackend); err != nil {
		return ApplicationIntent{}, err
	}
	serviceIDs := make(map[string]struct{}, len(intent.Services))
	for serviceIndex := range intent.Services {
		service := &intent.Services[serviceIndex]
		if _, duplicate := serviceIDs[service.ID]; duplicate {
			return ApplicationIntent{}, fmt.Errorf("Application Intent service id 重复: %q", service.ID)
		}
		serviceIDs[service.ID] = struct{}{}
		pluginIDs := make(map[string]struct{}, len(service.RootPlugins))
		for pluginIndex := range service.RootPlugins {
			selection := &service.RootPlugins[pluginIndex]
			if _, duplicate := pluginIDs[selection.Ref.PluginID]; duplicate {
				return ApplicationIntent{}, fmt.Errorf("Application Intent service %q root plugin 重复: %q", service.ID, selection.Ref.PluginID)
			}
			pluginIDs[selection.Ref.PluginID] = struct{}{}
			sort.Strings(selection.Features)
		}
		sort.Slice(service.RootPlugins, func(i, j int) bool {
			return service.RootPlugins[i].Ref.PluginID < service.RootPlugins[j].Ref.PluginID
		})
		if service.Operations.Autoscaling != nil {
			autoscaling := service.Operations.Autoscaling
			if autoscaling.MinReplicas > autoscaling.MaxReplicas {
				return ApplicationIntent{}, fmt.Errorf("Application Intent service %q autoscaling min_replicas 不能大于 max_replicas", service.ID)
			}
			if service.Operations.Replicas < autoscaling.MinReplicas || service.Operations.Replicas > autoscaling.MaxReplicas {
				return ApplicationIntent{}, fmt.Errorf("Application Intent service %q replicas 必须位于 autoscaling min/max 区间", service.ID)
			}
		}
	}
	sort.Slice(intent.Services, func(i, j int) bool { return intent.Services[i].ID < intent.Services[j].ID })
	return intent, nil
}
