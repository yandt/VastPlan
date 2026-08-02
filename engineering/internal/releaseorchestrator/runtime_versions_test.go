package releaseorchestrator

import (
	"os"
	"path/filepath"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestSyncSelectedPluginRuntimeVersionsUsesManifestAsTheSourceOfTruth(t *testing.T) {
	root := t.TempDir()
	pluginPath := "extensions/plugins/cn.vastplan.demo"
	sourcePath := filepath.Join(root, pluginPath, "backend", "main.go")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte(`package demo

const PluginVersion = "1.0.0"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	workspace := PluginWorkspace{Plugins: map[string]WorkspacePlugin{
		"cn.vastplan.demo": {ID: "cn.vastplan.demo", Version: "1.1.0", Path: pluginPath, Manifest: pluginv1.Manifest{ID: "cn.vastplan.demo", Version: "1.1.0"}},
	}}
	changes, err := SyncSelectedPluginRuntimeVersions(root, workspace, map[string]string{"cn.vastplan.demo": "1.1.0"}, true)
	if err != nil || len(changes) != 1 {
		t.Fatalf("runtime version 投影应更新一个 Go 常量: changes=%+v err=%v", changes, err)
	}
	raw, err := os.ReadFile(sourcePath)
	if err != nil || string(raw) != `package demo

const PluginVersion = "1.1.0"
` {
		t.Fatalf("runtime version 未按 Manifest 同步: %q err=%v", raw, err)
	}
}

func TestSyncSelectedPluginRuntimeVersionsSupportsLowercaseConstants(t *testing.T) {
	updated, changed, err := replacePluginRuntimeVersion([]byte(`const pluginVersion           = "1.0.0"`), "cn.vastplan.demo", "1.1.0")
	if err != nil || !changed || string(updated) != `const pluginVersion           = "1.1.0"` {
		t.Fatalf("lowercase runtime version 未同步: updated=%q changed=%v err=%v", updated, changed, err)
	}
}

func TestSyncSelectedPluginRuntimeVersionsSupportsIdentityTuple(t *testing.T) {
	updated, changed, err := replacePluginRuntimeVersion([]byte(`const id, version, capability = "cn.vastplan.demo", "1.0.0", "demo"`), "cn.vastplan.demo", "1.1.0")
	if err != nil || !changed || string(updated) != `const id, version, capability = "cn.vastplan.demo", "1.1.0", "demo"` {
		t.Fatalf("identity tuple runtime version 未同步: updated=%q changed=%v err=%v", updated, changed, err)
	}
}
