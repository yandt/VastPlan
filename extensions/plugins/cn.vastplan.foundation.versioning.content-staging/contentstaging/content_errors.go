package contentstaging

import (
	"errors"

	contentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioncontent/v1"
)

type ContentReferenceError struct {
	Code      string
	Retryable bool
	Err       error
}

func (e *ContentReferenceError) Error() string {
	if e == nil || e.Err == nil {
		return "Content Reference 操作失败"
	}
	return e.Err.Error()
}

func (e *ContentReferenceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func contentReferenceError(code string, retryable bool, err error) error {
	if err == nil {
		err = errors.New("Content Reference 操作失败")
	}
	return &ContentReferenceError{Code: code, Retryable: retryable, Err: err}
}

func ContentReferenceErrorDetails(err error) (string, bool) {
	var target *ContentReferenceError
	if errors.As(err, &target) && contentv1.KnownErrorCode(target.Code) {
		return target.Code, target.Retryable
	}
	return contentv1.ErrorStorageUnavailable, true
}
