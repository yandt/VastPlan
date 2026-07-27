package arch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrontendRuntimeProtocolIsSelectedOnlyAtCompositionRoot(t *testing.T) {
	root := repoRoot(t)
	for path, forbidden := range map[string][]string{
		"core/kernels/frontend/src/portal-generation.ts":   {"runtimeProtocol?:", "?? productionFrontendRuntimeProtocol"},
		"core/kernels/frontend/src/portal-development.ts":  {"protocol: FrontendRuntimeProtocol ="},
		"core/kernels/frontend/src/portal-shell.tsx":       {"protocol = productionFrontendRuntimeProtocol", "function preparePortal("},
		"core/kernels/frontend/src/module-runtime-spec.ts": {"parsePortalRuntimeSpec", "parseDevelopmentRuntimeSpec"},
	} {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range forbidden {
			if strings.Contains(string(raw), value) {
				t.Errorf("Frontend Runtime 下层重新拥有协议默认值: %s 包含 %q", path, value)
			}
		}
	}
	raw, err := os.ReadFile(filepath.Join(root, "core", "kernels", "frontend", "src", "portal-shell.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"runtimeProtocol: runtimeSource.protocol", "fetchRuntimeSpec(fetcher, recoveryEndpoint, pathname, runtimeSource.protocol)"} {
		if !strings.Contains(string(raw), required) {
			t.Errorf("首次、热替换与恢复没有显式复用组合根协议: 缺少 %q", required)
		}
	}
}
