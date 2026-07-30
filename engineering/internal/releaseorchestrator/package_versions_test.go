package releaseorchestrator

import (
	"os"
	"path/filepath"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestSyncSelectedPluginPackageVersionsOnlyChangesSelectedPlugin(t *testing.T) {
	root := t.TempDir()
	pluginPath := "extensions/plugins/cn.vastplan.demo"
	packagePath := filepath.Join(root, pluginPath, "frontend", "package.json")
	if err := os.MkdirAll(filepath.Dir(packagePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packagePath, []byte("{\n  \"name\": \"demo\",\n  \"version\": \"1.0.0\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspace := PluginWorkspace{Plugins: map[string]WorkspacePlugin{
		"cn.vastplan.demo": {ID: "cn.vastplan.demo", Version: "1.1.0", Path: pluginPath, Manifest: pluginv1.Manifest{ID: "cn.vastplan.demo", Version: "1.1.0"}},
	}}
	changes, err := SyncSelectedPluginPackageVersions(root, workspace, map[string]string{"cn.vastplan.demo": "1.1.0"}, true)
	if err != nil || len(changes) != 1 {
		t.Fatalf("package version 应产生一个投影改动: changes=%+v err=%v", changes, err)
	}
	raw, err := os.ReadFile(packagePath)
	if err != nil || string(raw) != "{\n  \"name\": \"demo\",\n  \"version\": \"1.1.0\"\n}\n" {
		t.Fatalf("package.json 布局或版本错误: %s err=%v", raw, err)
	}
}
