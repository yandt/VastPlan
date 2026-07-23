package pluginsbom

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactsupplychain"
)

func TestGenerateGoSBOMFromActualBuildInfo(t *testing.T) {
	root := t.TempDir()
	plugin := filepath.Join(root, "extensions", "plugins", "cn.vastplan.go-test")
	if err := os.MkdirAll(plugin, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"cn.vastplan.go-test","name":"test","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"backend":"^1.0"},"execution":{"backend":{"driver":"native","requirements":{}}},"activation":["onStartup"],"entry":{"backend":"backend/plugin"},"contributes":{"backend":{"tools":[]}}}`
	if err := os.WriteFile(filepath.Join(plugin, "vastplan.plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	first, err := Generate(Options{Root: root, PluginDir: plugin, GoBinaries: []string{binary}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(Options{Root: root, PluginDir: plugin, GoBinaries: []string{binary}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Raw, second.Raw) {
		t.Fatal("相同 Go 构建事实没有生成确定性 SBOM")
	}
	summary, err := artifactsupplychain.InspectCycloneDX(first.Raw)
	if err != nil || summary.RootName != "cn.vastplan.go-test" {
		t.Fatalf("Go SBOM 无效: summary=%+v err=%v", summary, err)
	}
}

func TestGeneratePythonSBOMFromSignedRequirements(t *testing.T) {
	root := t.TempDir()
	plugin := filepath.Join(root, "extensions", "plugins", "cn.vastplan.python-test")
	if err := os.MkdirAll(plugin, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"cn.vastplan.python-test","name":"test","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"backend":"^1.0"},"execution":{"backend":{"driver":"python","requirements":{"python":">=3.11","grpcio":"1.70.0"}}},"activation":["onStartup"],"entry":{"backend":"backend/main.py"},"contributes":{"backend":{"tools":[]}}}`
	if err := os.WriteFile(filepath.Join(plugin, "vastplan.plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Generate(Options{Root: root, PluginDir: plugin})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := artifactsupplychain.InspectCycloneDX(result.Raw)
	if err != nil || summary.Components != 1 || summary.RootName != "cn.vastplan.python-test" {
		t.Fatalf("Python SBOM 无效: summary=%+v err=%v raw=%s", summary, err, result.Raw)
	}
}

func TestGenerateNodeSBOMFromActualMetafile(t *testing.T) {
	root := t.TempDir()
	plugin := filepath.Join(root, "extensions", "plugins", "cn.vastplan.node-test")
	dependency := filepath.Join(root, "extensions", "sdk", "node", "example")
	for _, directory := range []string{plugin, filepath.Join(plugin, "backend"), dependency, filepath.Join(dependency, "src")} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `{"id":"cn.vastplan.node-test","name":"test","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"backend":"^1.0"},"execution":{"backend":{"driver":"node-worker","requirements":{"node":">=20"},"node":{"workerSafe":true,"moduleFormat":"esm"}}},"activation":["onStartup"],"entry":{"backend":"backend/main.mjs"},"contributes":{"backend":{"tools":[]}}}`
	if err := os.WriteFile(filepath.Join(plugin, "vastplan.plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dependency, "package.json"), []byte(`{"name":"@vastplan/example","version":"2.0.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	metadata := map[string]any{"inputs": map[string]any{"extensions/plugins/cn.vastplan.node-test/backend/main.mjs": map[string]any{}, "extensions/sdk/node/example/src/index.mjs": map[string]any{}}, "outputs": map[string]any{"out.mjs": map[string]any{}}}
	raw, _ := json.Marshal(metadata)
	metafile := filepath.Join(root, "meta.json")
	if err := os.WriteFile(metafile, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Generate(Options{Root: root, PluginDir: plugin, Metafiles: []string{metafile}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Components != 1 {
		t.Fatalf("Node SBOM 应包含实际 bundle workspace 依赖: %s", result.Raw)
	}
}
