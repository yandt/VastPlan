package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestCopyTreeSkipsPythonBytecode(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "main.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(source, "__pycache__")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "main.pyc"), []byte("bytecode"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(source, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "main.py")); err != nil {
		t.Fatal("Python 源文件必须进入制品")
	}
	if _, err := os.Stat(filepath.Join(target, "__pycache__")); !os.IsNotExist(err) {
		t.Fatalf("Python 字节码缓存不得进入制品: %v", err)
	}
}

func TestStagePackageInjectsSignedDynamicGoFingerprint(t *testing.T) {
	source := t.TempDir()
	manifest := []byte(`{
  "id":"com.vastplan.foundation.test.dynamic","name":"dynamic","description":"dynamic","version":"1.0.0","publisher":"vastplan",
  "engines":{"backend":"^1.0"},"execution":{"backend":{"driver":"native","minimumIsolation":"trusted-process",
    "dynamicGo":{"entry":"backend/plugin.so","abi":"vastplan.dynamic-go.v1","required":true}}},
  "activation":["onStartup"],"entry":{"backend":"backend/plugin"},
  "contributes":{"backend":{"tools":[{"id":"foundation.test.dynamic.tool","service_role":"backend"}]}}
}`)
	if err := os.WriteFile(filepath.Join(source, "vastplan.plugin.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	backend, dynamic := filepath.Join(t.TempDir(), "plugin"), filepath.Join(t.TempDir(), "plugin.so")
	if err := os.WriteFile(backend, []byte("process"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dynamic, []byte("module"), 0o644); err != nil {
		t.Fatal(err)
	}
	fingerprint := strings.Repeat("a", 64)
	staged, cleanup := stagePackage(source, backend, "", dynamic, fingerprint, "", "")
	defer cleanup()
	raw, err := os.ReadFile(filepath.Join(staged, "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Execution.Backend.DynamicGo.Fingerprint != fingerprint || !got.Execution.Backend.DynamicGo.Required {
		t.Fatalf("签名清单没有冻结 dynamic-go 构建指纹: %+v", got.Execution.Backend.DynamicGo)
	}
}

func TestStagePackageInjectsFrontendBundleAtManifestEntry(t *testing.T) {
	source := t.TempDir()
	manifest := []byte(`{
  "id":"com.vastplan.product.test.frontend","name":"frontend","description":"frontend","version":"1.0.0","publisher":"vastplan",
  "engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/dist/index.js"},
  "contributes":{"frontend":{"views":[{"id":"test.frontend","title":"Test"}]}}
}`)
	if err := os.WriteFile(filepath.Join(source, "vastplan.plugin.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "index.js")
	content := []byte("export default { register() {} };\n")
	if err := os.WriteFile(bundle, content, 0o644); err != nil {
		t.Fatal(err)
	}
	staged, cleanup := stagePackage(source, "", bundle, "", "", "", "")
	defer cleanup()
	got, err := os.ReadFile(filepath.Join(staged, "frontend", "dist", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("frontend bundle 注入内容不一致: %q", got)
	}
}
