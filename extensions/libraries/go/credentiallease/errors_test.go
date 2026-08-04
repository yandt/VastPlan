package credentiallease

import (
	"errors"
	"testing"
)

func TestFailurePreservesStableCodeAndRetryability(t *testing.T) {
	cause := errors.New("trusted diagnostic")
	err := NewFailure(ErrorMaterialUnavailable, false, cause)
	code, retryable, ok := FailureDetails(err)
	if !ok || code != ErrorMaterialUnavailable || retryable || !errors.Is(err, cause) {
		t.Fatalf("Material Lease Failure 未保留稳定语义: code=%q retryable=%v ok=%v err=%v", code, retryable, ok, err)
	}
}

func TestFailureRejectsUnknownCodes(t *testing.T) {
	err := NewFailure("plugin.private.detail", false, nil)
	code, retryable, ok := FailureDetails(err)
	if !ok || code != ErrorServiceUnavailable || !retryable || err.Error() != SafeFailureMessage(ErrorServiceUnavailable) {
		t.Fatalf("未知错误码必须降级为安全兜底: code=%q retryable=%v ok=%v err=%v", code, retryable, ok, err)
	}
}
