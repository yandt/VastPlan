package databaseruntime

import (
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/credentiallease"
)

func classifyCredentialLeaseError(err error) (error, bool) {
	code, retryable, ok := credentiallease.FailureDetails(err)
	if !ok {
		return nil, false
	}
	switch code {
	case credentiallease.ErrorReferenceUnavailable, credentiallease.ErrorMaterialUnavailable, credentiallease.ErrorChanged:
		return NewRuntimeError(databasev1.ErrorCredentialUnavailable, false, err), true
	default:
		return NewRuntimeError(databasev1.ErrorCredentialServiceUnavailable, retryable, err), true
	}
}
