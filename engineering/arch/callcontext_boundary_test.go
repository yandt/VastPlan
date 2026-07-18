package arch

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestBackendPluginManifestsDeclareContextAccess(t *testing.T) {
	pluginsDir := filepath.Join(repoRoot(t), "extensions", "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(pluginsDir, entry.Name(), "vastplan.plugin.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := pluginv1.ParseManifest(raw)
		if err != nil {
			t.Fatalf("解析插件 %s: %v", entry.Name(), err)
		}
		if _, backend := manifest.Engines["backend"]; backend && manifest.ContextAccess == nil {
			t.Errorf("Backend 插件 %s 必须显式声明 contextAccess，不能继承宽泛默认值", manifest.ID)
		}
	}
}

func TestHostControlDataDoesNotReturnToCallContextMetadata(t *testing.T) {
	root := repoRoot(t)
	reserved := [][]byte{[]byte("vastplan.internal."), []byte("vastplan.transport.")}
	for _, tree := range []string{"core", "extensions"} {
		err := filepath.WalkDir(filepath.Join(root, tree), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "node_modules" || d.Name() == "__pycache__" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, ".pb.go") ||
				strings.HasSuffix(path, "_pb2.py") || path == filepath.Join(root, "core", "shared", "go", "callcontext", "trusted.go") {
				return nil
			}
			if !strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".py") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, prefix := range reserved {
				if bytes.Contains(raw, prefix) {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s 重新使用宿主保留 metadata 前缀 %q；控制信息必须留在专用信封/provenance", rel, prefix)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
