package interaction

import (
	uiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/ui/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/interactionapi"
)

func copyRecord(record interactionapi.Record) interactionapi.Record {
	copy := record
	copy.Request.EligibleSubjects = append([]string(nil), record.Request.EligibleSubjects...)
	copy.Request.AllowedSurfaces = append([]uiv1.InteractionSurface(nil), record.Request.AllowedSurfaces...)
	copy.Audit = append([]interactionapi.AuditEvent(nil), record.Audit...)
	if record.Response != nil {
		response := copyResponse(*record.Response)
		copy.Response = &response
	}
	return copy
}

func copyResponse(response uiv1.InteractionResponse) uiv1.InteractionResponse {
	copy := response
	copy.Values = mapsClone(response.Values)
	copy.CredentialRef = mapsClone(response.CredentialRef)
	return copy
}

func mapsClone[T any](value map[string]T) map[string]T {
	if value == nil {
		return nil
	}
	copy := make(map[string]T, len(value))
	for key, item := range value {
		copy[key] = item
	}
	return copy
}
