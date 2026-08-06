package platformcontrol

import (
	"errors"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

// Failure preserves a stable, value-free Database Runtime diagnosis across
// the transport-neutral Platform Control port. Raw provider errors never leave
// the Database Runtime process.
type Failure struct {
	Code      string
	Retryable bool
}

func (e *Failure) Error() string {
	if e == nil || !databasev1.KnownErrorCode(e.Code) {
		return "Platform Control database operation failed"
	}
	return e.Code
}

func NewFailure(code string, retryable bool) error {
	if !databasev1.KnownErrorCode(code) {
		return nil
	}
	return &Failure{Code: code, Retryable: retryable}
}

func FailureDetails(err error) (code string, retryable bool, ok bool) {
	var failure *Failure
	if !errors.As(err, &failure) || failure == nil || !databasev1.KnownErrorCode(failure.Code) {
		return "", false, false
	}
	return failure.Code, failure.Retryable, true
}
