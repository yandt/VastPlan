package credentialbroker

import (
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/credentiallease"
)

func leaseFailureFromResult(result *contractv1.CallResult) error {
	if result == nil || result.GetError() == nil {
		return credentiallease.NewFailure(credentiallease.ErrorServiceUnavailable, true, errors.New("凭证服务未返回 Material Lease 结果"))
	}
	code := result.GetError().GetCode()
	retryable := result.GetError().GetRetryable()
	if !credentiallease.KnownFailureCode(code) {
		code, retryable = credentiallease.ErrorServiceUnavailable, true
	}
	return credentiallease.NewFailure(code, retryable, errors.New(credentiallease.SafeFailureMessage(code)))
}
