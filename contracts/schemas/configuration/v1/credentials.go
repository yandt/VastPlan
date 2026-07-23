package configurationv1

import (
	"sort"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
)

// MergeManagedCredentials applies candidate replacements over the controller's
// complete Active set. Omitted fields are retained; plugin-settings never asks
// the controller to disclose the resulting handles.
func MergeManagedCredentials(active, replacements map[string]commonv1.ManagedCredentialRef) map[string]commonv1.ManagedCredentialRef {
	merged := make(map[string]commonv1.ManagedCredentialRef, len(active)+len(replacements))
	for fieldID, ref := range active {
		merged[fieldID] = ref
	}
	for fieldID, ref := range replacements {
		merged[fieldID] = ref
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

// ReplacedManagedCredentials returns old references no longer selected by the
// merged Active set. A controller persists this retirement outbox with its
// committed candidate and retries retirement without exposing the references.
func ReplacedManagedCredentials(active, merged map[string]commonv1.ManagedCredentialRef) []commonv1.ManagedCredentialRef {
	fields := make([]string, 0, len(active))
	for fieldID := range active {
		fields = append(fields, fieldID)
	}
	sort.Strings(fields)
	retired := make([]commonv1.ManagedCredentialRef, 0)
	for _, fieldID := range fields {
		previous := active[fieldID]
		current, retained := merged[fieldID]
		if !retained || current.Handle != previous.Handle {
			retired = append(retired, previous)
		}
	}
	return retired
}
