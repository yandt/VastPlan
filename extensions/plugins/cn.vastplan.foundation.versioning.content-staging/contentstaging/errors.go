package contentstaging

import (
	"errors"

	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
)

type StagingError struct {
	Code      string
	Retryable bool
	Err       error
}

func (e *StagingError) Error() string {
	if e == nil || e.Err == nil {
		return "Content Staging 操作失败"
	}
	return e.Err.Error()
}

func (e *StagingError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func stagingError(code string, retryable bool, err error) error {
	if err == nil {
		err = errors.New("Content Staging 操作失败")
	}
	return &StagingError{Code: code, Retryable: retryable, Err: err}
}

func ErrorDetails(err error) (string, bool) {
	var target *StagingError
	if errors.As(err, &target) && stagingv1.KnownErrorCode(target.Code) {
		return target.Code, target.Retryable
	}
	return stagingv1.ErrorStorageUnavailable, true
}

var errStreamLimitExceeded = errors.New("上传字节超过声明大小")
