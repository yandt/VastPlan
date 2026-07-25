package planner

import (
	"fmt"
	"sort"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
)

type credentialBindings map[string]map[string]map[string]commonv1.ManagedCredentialRef

func normalizeConfigurationSnapshot(snapshot *backendcompositionv1.PlanningConfigurationSnapshot) (credentialBindings, error) {
	result := credentialBindings{}
	if snapshot == nil {
		return result, nil
	}
	if snapshot.Version != 1 || snapshot.Digest != snapshot.ComputedDigest() {
		return nil, fmt.Errorf("Configuration Snapshot 版本或摘要无效")
	}
	bindings := append([]backendcompositionv1.PlanningCredentialBinding(nil), snapshot.Bindings...)
	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].UnitID != bindings[j].UnitID {
			return bindings[i].UnitID < bindings[j].UnitID
		}
		if bindings[i].PluginID != bindings[j].PluginID {
			return bindings[i].PluginID < bindings[j].PluginID
		}
		return bindings[i].FieldID < bindings[j].FieldID
	})
	for _, binding := range bindings {
		if binding.UnitID == "" || binding.PluginID == "" || binding.FieldID == "" || commonv1.ValidateManagedCredentialRef(binding.Ref) != nil {
			return nil, fmt.Errorf("Configuration Snapshot 包含无效 CredentialRef 绑定")
		}
		if binding.Ref.Owner != binding.PluginID {
			return nil, fmt.Errorf("Configuration Snapshot 插件 %s 不能使用其他 owner 的 CredentialRef", binding.PluginID)
		}
		if result[binding.UnitID] == nil {
			result[binding.UnitID] = map[string]map[string]commonv1.ManagedCredentialRef{}
		}
		if result[binding.UnitID][binding.PluginID] == nil {
			result[binding.UnitID][binding.PluginID] = map[string]commonv1.ManagedCredentialRef{}
		}
		if _, duplicate := result[binding.UnitID][binding.PluginID][binding.FieldID]; duplicate {
			return nil, fmt.Errorf("Configuration Snapshot CredentialRef 绑定重复: %s/%s/%s", binding.UnitID, binding.PluginID, binding.FieldID)
		}
		result[binding.UnitID][binding.PluginID][binding.FieldID] = binding.Ref
	}
	return result, nil
}
