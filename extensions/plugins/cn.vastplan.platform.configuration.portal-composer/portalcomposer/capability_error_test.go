package portalcomposer

import (
	"errors"
	"strings"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
)

func TestCatalogCallErrorPreservesTrustedHostReason(t *testing.T) {
	result := &contractv1.CallResult{
		Status: contractv1.CallResult_STATUS_ERROR,
		Error:  &contractv1.Error{Code: "artifact.invalid", Message: "frontend manifest mismatch"},
	}
	if got := catalogCallError(result, nil).Error(); !strings.Contains(got, "artifact.invalid") || !strings.Contains(got, "frontend manifest mismatch") {
		t.Fatalf("可信宿主拒绝原因丢失: %q", got)
	}
	transport := errors.New("transport unavailable")
	if got := catalogCallError(result, transport); !errors.Is(got, transport) {
		t.Fatalf("传输错误必须优先保留: %v", got)
	}
}
