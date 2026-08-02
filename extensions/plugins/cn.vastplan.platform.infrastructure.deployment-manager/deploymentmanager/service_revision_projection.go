package deploymentmanager

import (
	"encoding/json"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginconfig"
)

func cloneServiceRevision(in platformadminapi.ServiceRevision) platformadminapi.ServiceRevision {
	raw, _ := json.Marshal(in)
	var out platformadminapi.ServiceRevision
	_ = json.Unmarshal(raw, &out)
	return out
}

func cloneServiceRevisions(in []platformadminapi.ServiceRevision) []platformadminapi.ServiceRevision {
	out := make([]platformadminapi.ServiceRevision, len(in))
	for i := range in {
		out[i] = cloneServiceRevision(in[i])
	}
	return out
}

// publicServiceRevision removes credential handles at the protocol boundary.
// The persisted control-plane revision still carries them so Node Agent can
// project the exact reference to the authenticated owning plugin.
func publicServiceRevision(in platformadminapi.ServiceRevision) platformadminapi.ServiceRevision {
	out := cloneServiceRevision(in)
	out.ConfigurationSnapshot = nil
	for index := range out.Composition.Units {
		delete(out.Composition.Units[index].Spec.Config, pluginconfig.ManagedCredentialsKey)
	}
	if out.ResolutionReport != nil && out.ResolutionReport.ApplicationComposition != nil {
		for index := range out.ResolutionReport.ApplicationComposition.Units {
			delete(out.ResolutionReport.ApplicationComposition.Units[index].Spec.Config, pluginconfig.ManagedCredentialsKey)
		}
	}
	for index := range out.Preview.Units {
		delete(out.Preview.Units[index].Config, pluginconfig.ManagedCredentialsKey)
	}
	return out
}

func publicServiceRevisions(in []platformadminapi.ServiceRevision) []platformadminapi.ServiceRevision {
	out := make([]platformadminapi.ServiceRevision, len(in))
	for index := range in {
		out[index] = publicServiceRevision(in[index])
	}
	return out
}
