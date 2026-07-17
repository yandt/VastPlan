package nodeagent

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDynamicGoLoaderRejectsSignedFingerprintBeforeOpeningModule(t *testing.T) {
	const host = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	_, err := NewDynamicGoLoader(host).Load(filepath.Join(t.TempDir(), "does-not-exist.so"),
		"com.vastplan.foundation.test.dynamic", "1.0.0",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err == nil || !strings.Contains(err.Error(), "签名构建指纹不一致") {
		t.Fatalf("必须在访问 .so 前拒绝签名指纹不匹配: %v", err)
	}
}
