package credentials

import (
	"errors"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/credentiallease"
)

var (
	errMaterialLeaseDenied          = errors.New("material lease caller 未获授权")
	errManagedCredentialUnavailable = errors.New("托管凭证不存在、未激活或引用不匹配")
	errManagedCredentialChanged     = errors.New("托管凭证在 lease 签发期间已变化")
)

func classifyTransitLeaseFailure(err error) error {
	var transitError *VaultTransitError
	if errors.As(err, &transitError) {
		if transitError.InvalidMaterial {
			return credentiallease.NewFailure(credentiallease.ErrorMaterialUnavailable, false, err)
		}
		return credentiallease.NewFailure(credentiallease.ErrorServiceUnavailable, transitError.Retryable, err)
	}
	return credentiallease.NewFailure(credentiallease.ErrorServiceUnavailable, true, err)
}

func classifyMaterialLeaseFailure(err error) error {
	if err == nil {
		return nil
	}
	if _, _, ok := credentiallease.FailureDetails(err); ok {
		return err
	}
	switch {
	case errors.Is(err, errMaterialLeaseDenied):
		return credentiallease.NewFailure(credentiallease.ErrorDenied, false, err)
	case errors.Is(err, errManagedCredentialUnavailable):
		return credentiallease.NewFailure(credentiallease.ErrorReferenceUnavailable, false, err)
	case errors.Is(err, errManagedCredentialChanged):
		return credentiallease.NewFailure(credentiallease.ErrorChanged, false, err)
	default:
		return credentiallease.NewFailure(credentiallease.ErrorServiceUnavailable, true, err)
	}
}
