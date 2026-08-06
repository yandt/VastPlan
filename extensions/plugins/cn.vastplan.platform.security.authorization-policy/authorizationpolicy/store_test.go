package authorizationpolicy

import "testing"

func TestFileBootstrapStateReaderIsNotRuntimeStore(t *testing.T) {
	reader := &FileBootstrapStateReader{Path: "/var/lib/vastplan/authorization-policy.json"}
	if _, mutable := any(reader).(Store); mutable {
		t.Fatal("Bootstrap 文件读取器不得实现可写 Runtime Store")
	}
}
