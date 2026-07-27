package arch

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var concreteSDKRole = regexp.MustCompile(`(?m)^type\s+[A-Za-z0-9_]*(?:Provider|Engine|Repository|Broker)\s+struct\s*\{`)

func TestArch_SDKDoesNotOwnConcreteProviderImplementations(t *testing.T) {
	root := repoRoot(t)
	err := filepath.WalkDir(filepath.Join(root, "extensions", "sdk"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "node_modules" || entry.Name() == "dist" || entry.Name() == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		if concreteSDKRole.Match(raw) || strings.Contains(string(raw), `"os/exec"`) {
			t.Errorf("SDK 拥有具体 Provider/Engine 或进程实现: %s；具体实现必须归属插件（AGENTS.md）", filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestArch_EveryProductAndExamplePluginHasLocalReadme(t *testing.T) {
	root := repoRoot(t)
	for _, relativeRoot := range []string{"extensions/plugins", "examples/plugins"} {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(relativeRoot)))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(root, filepath.FromSlash(relativeRoot), entry.Name(), "README.md")
			raw, err := os.ReadFile(path)
			if err != nil || len(strings.TrimSpace(string(raw))) < 80 {
				t.Errorf("插件缺少有效本地 README: %s", filepath.ToSlash(path))
			}
		}
	}
}
