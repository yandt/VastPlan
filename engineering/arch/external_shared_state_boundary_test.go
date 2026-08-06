package arch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const localFileBoundaryMarker = "vastplan:local-file-boundary "

var allowedLocalFileBoundaries = map[string]struct{}{
	"bootstrap-root":     {},
	"derived-projection": {},
	"provider-private":   {},
	"recovery-root":      {},
}

// External Shared State plugins must not silently regain a local writable
// truth source. A narrow Bootstrap/Recovery/Provider boundary or derived
// projection is allowed only when the owning file declares the audited class.
func TestExternalSharedStatePluginsHaveNoUnclassifiedLocalWrites(t *testing.T) {
	root := repoRoot(t)
	inventory := readStateOwnershipInventory(t, root)
	auditedBoundaries := make(map[string]map[string]struct{}, len(inventory.StateOwnership))
	for _, ownership := range inventory.StateOwnership {
		auditedBoundaries[ownership.ID] = stringSet(ownership.LocalBoundaries...)
	}
	pluginsRoot := filepath.Join(root, "extensions", "plugins")
	entries, err := os.ReadDir(pluginsRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pluginRoot := filepath.Join(pluginsRoot, entry.Name())
		if !externalSharedStateManifest(t, filepath.Join(pluginRoot, "vastplan.plugin.json")) {
			continue
		}
		err := filepath.WalkDir(pluginRoot, func(path string, item os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if item.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			source := string(raw)
			if !containsLocalWrite(source) {
				return nil
			}
			boundary := declaredLocalFileBoundary(source)
			if _, allowed := allowedLocalFileBoundaries[boundary]; !allowed {
				t.Errorf("external-shared 插件存在未分类本机写入: %s", filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator))))
			} else if _, audited := auditedBoundaries[entry.Name()][boundary]; !audited {
				t.Errorf("external-shared 插件的本机写入未进入状态归属清单: plugin=%s boundary=%s", entry.Name(), boundary)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func externalSharedStateManifest(t *testing.T, path string) bool {
	t.Helper()
	raw, err := os.ReadFile(path)
	if errorsIsNotExist(err) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Runtime struct {
			StateModel string `json:"stateModel"`
		} `json:"runtime"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("解析 %s: %v", path, err)
	}
	return manifest.Runtime.StateModel == "external-shared"
}

func containsLocalWrite(source string) bool {
	for _, call := range []string{
		"os.WriteFile(", "os.Create(", "os.CreateTemp(", "os.OpenFile(",
		"os.Rename(", "os.Remove(", "os.RemoveAll(", "os.MkdirAll(",
	} {
		if strings.Contains(source, call) {
			return true
		}
	}
	return false
}

func declaredLocalFileBoundary(source string) string {
	index := strings.Index(source, localFileBoundaryMarker)
	if index < 0 {
		return ""
	}
	value := source[index+len(localFileBoundaryMarker):]
	if end := strings.IndexAny(value, " \t\r\n"); end >= 0 {
		value = value[:end]
	}
	return value
}

func errorsIsNotExist(err error) bool { return err != nil && os.IsNotExist(err) }
