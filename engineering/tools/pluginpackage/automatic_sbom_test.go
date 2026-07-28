package main

import (
	"os"
	"path/filepath"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactsupplychain"
)

func TestGenerateAutomaticSBOMForPythonPlugin(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{"go.mod": "module example.test\n", "pnpm-workspace.yaml": "packages: []\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	plugin := filepath.Join(root, "extensions", "plugins", "cn.vastplan.python-auto")
	if err := os.MkdirAll(plugin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(plugin, "backend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plugin, "backend", "main.py"), []byte("print('hello')\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"cn.vastplan.python-auto","name":"auto","description":"auto","version":"1.0.0","publisher":"vastplan","engines":{"backend":"^1.0"},"execution":{"backend":{"driver":"python","requirements":{"python":">=3.11","protobuf":"4.25.8"}}},"activation":["onStartup"],"entry":{"backend":"backend/main.py"},"contributes":{"backend":{"tools":[]}}}`
	if err := os.WriteFile(filepath.Join(plugin, "vastplan.plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	lockDirectory := filepath.Join(plugin, "supply-chain")
	if err := os.MkdirAll(lockDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	lock := `lock-version="1.0"
requires-python=">=3.11"
created-by="test"
packages=[{name="protobuf",version="4.25.8",wheels=[{path="python-wheels/protobuf-4.25.8-py3-none-any.whl",size=1,hashes={sha256="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}]}]
`
	if err := os.WriteFile(filepath.Join(lockDirectory, "pylock.toml"), []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}
	filename, cleanup, err := generateAutomaticSBOM(automaticSBOMInputs{Source: plugin})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := artifactsupplychain.InspectCycloneDX(raw)
	if err != nil || summary.RootName != "cn.vastplan.python-auto" || summary.Components != 1 {
		t.Fatalf("自动 SBOM 无效: summary=%+v err=%v", summary, err)
	}
}

func TestValidateSBOMDependencySourceRequiresSameSignedIdentity(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"cn.vastplan.demo","name":"demo","description":"demo","version":"1.2.3","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/dist/index.js"},"contributes":{"frontend":{"views":[]}}}`
	if err := os.WriteFile(filepath.Join(source, "vastplan.plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	original, err := pluginv1.ParseManifest([]byte(manifest))
	if err != nil {
		t.Fatal(err)
	}
	candidate := original
	candidate.Version = "1.2.3-dev.workspace.0123456789abcdef"
	if err := validateSBOMDependencySource(candidate, source); err != nil {
		t.Fatalf("同一稳定基础版本应允许只读依赖解析: %v", err)
	}
	candidate.ID = "cn.vastplan.other"
	if err := validateSBOMDependencySource(candidate, source); err == nil {
		t.Fatal("不同插件身份不得复用依赖证据")
	}
}
