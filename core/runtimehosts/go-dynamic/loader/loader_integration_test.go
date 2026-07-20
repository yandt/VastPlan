//go:build (linux || darwin || freebsd) && cgo

package loader

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

func TestLoaderLoadsRealFirstPartyModule(t *testing.T) {
	const fingerprint = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if raceBuild() {
		t.Skip("Go 官方 plugin 机制不可靠支持 race detector")
	}
	output := filepath.Join(t.TempDir(), "bootstrap-policy.so")
	command := exec.Command("go", "build", "-buildmode=plugin",
		"-ldflags", "-X main.dynamicGoBuildFingerprint="+fingerprint,
		"-o", output,
		"../../../../extensions/plugins/cn.vastplan.foundation.security.bootstrap-policy/dynamic")
	command.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "go-cache"))
	if raw, err := command.CombinedOutput(); err != nil {
		t.Fatalf("构建真实 dynamic-go 模块: %v\n%s", err, raw)
	}
	loader := New(fingerprint)
	definition, err := loader.Load(output,
		"cn.vastplan.foundation.security.bootstrap-policy", "0.1.0", fingerprint)
	if err != nil {
		t.Fatalf("加载真实 dynamic-go 模块: %v", err)
	}
	if definition.ID != "cn.vastplan.foundation.security.bootstrap-policy" || len(definition.Contributions) != 2 {
		t.Fatalf("dynamic-go 模块定义不完整: %+v", definition)
	}
	secondPath := filepath.Join(t.TempDir(), "bootstrap-policy-v2.so")
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loader.Load(secondPath, definition.ID, definition.Version, fingerprint); err == nil ||
		!strings.Contains(err.Error(), "新 generation") {
		t.Fatalf("dynamic-go 换路径升级必须创建新 Host generation: %v", err)
	}
}

func raceBuild() bool {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return false
	}
	for _, setting := range info.Settings {
		if setting.Key == "-race" && setting.Value == "true" {
			return true
		}
	}
	return false
}
