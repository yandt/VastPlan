package deploymentmanager

import (
	"encoding/json"
	"errors"
	"sort"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/configurationactivation"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginconfiguration"
)

func patchIntentConfiguration(active platformadminapi.ServiceRevision, definition pluginconfiguration.Definition, request configurationactivation.CreateRequest) (backendcompositionv1.ApplicationIntent, *backendcompositionv1.PlanningConfigurationSnapshot, error) {
	if active.Intent == nil || active.ResolutionReport == nil {
		return backendcompositionv1.ApplicationIntent{}, nil, errors.New("活动修订没有 Application Intent")
	}
	intent := cloneJSON(*active.Intent)
	matched := false
	for index := range intent.Services {
		service := &intent.Services[index]
		if service.ID != definition.UnitID {
			continue
		}
		if service.PluginConfig == nil {
			service.PluginConfig = map[string]map[string]any{}
		}
		var values map[string]any
		if json.Unmarshal(request.Values, &values) != nil || values == nil {
			return backendcompositionv1.ApplicationIntent{}, nil, errInvalid
		}
		service.PluginConfig[definition.PluginID] = values
		matched = true
		break
	}
	if !matched {
		return backendcompositionv1.ApplicationIntent{}, nil, errNotFound
	}
	snapshot := clonePlanningSnapshot(active.ConfigurationSnapshot)
	if snapshot == nil {
		snapshot = &backendcompositionv1.PlanningConfigurationSnapshot{Version: 1, Bindings: []backendcompositionv1.PlanningCredentialBinding{}}
	}
	byField := map[string]backendcompositionv1.PlanningCredentialBinding{}
	for _, binding := range snapshot.Bindings {
		key := binding.UnitID + "\x00" + binding.PluginID + "\x00" + binding.FieldID
		byField[key] = binding
	}
	for fieldID, ref := range request.Credentials {
		binding := backendcompositionv1.PlanningCredentialBinding{UnitID: definition.UnitID, PluginID: definition.PluginID, FieldID: fieldID, Ref: ref}
		byField[binding.UnitID+"\x00"+binding.PluginID+"\x00"+binding.FieldID] = binding
	}
	snapshot.Bindings = snapshot.Bindings[:0]
	for _, binding := range byField {
		snapshot.Bindings = append(snapshot.Bindings, binding)
	}
	sort.Slice(snapshot.Bindings, func(i, j int) bool {
		left, right := snapshot.Bindings[i], snapshot.Bindings[j]
		if left.UnitID != right.UnitID {
			return left.UnitID < right.UnitID
		}
		if left.PluginID != right.PluginID {
			return left.PluginID < right.PluginID
		}
		return left.FieldID < right.FieldID
	})
	snapshot.Digest = snapshot.ComputedDigest()
	return intent, snapshot, nil
}
