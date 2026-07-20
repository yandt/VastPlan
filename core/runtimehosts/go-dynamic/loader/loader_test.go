package loader

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoaderRejectsSignedFingerprintBeforeOpeningModule(t *testing.T) {
	host := strings.Repeat("a", 64)
	_, err := New(host).Load(filepath.Join(t.TempDir(), "does-not-exist.so"),
		"cn.vastplan.foundation.test.module", "1.0.0", strings.Repeat("b", 64))
	if err == nil || !strings.Contains(err.Error(), "签名构建指纹不一致") {
		t.Fatalf("必须在 plugin.Open 前拒绝签名指纹漂移: %v", err)
	}
}
