package errorcode

import (
	"sort"
	"testing"
)

func TestKernelCodesAreNamespacedAndUnique(t *testing.T) {
	codes := KernelCodes()
	sort.Strings(codes)
	for i, code := range codes {
		if !Valid(code) {
			t.Errorf("内核错误码不符合命名空间规则: %q", code)
		}
		if !KernelDefined(code) {
			t.Errorf("KernelCodes 返回了未登记错误码: %q", code)
		}
		if i > 0 && codes[i-1] == code {
			t.Errorf("重复错误码: %q", code)
		}
	}
}

func TestValidRequiresLowercaseDottedNamespace(t *testing.T) {
	for _, code := range []string{"permission.denied", "plugin.handler_error", "vendor.subsystem.failed"} {
		if !Valid(code) {
			t.Errorf("合法错误码被拒绝: %q", code)
		}
	}
	for _, code := range []string{"", "denied", "Permission.Denied", "permission-denied", ".permission.denied"} {
		if Valid(code) {
			t.Errorf("非法错误码被接受: %q", code)
		}
	}
}
