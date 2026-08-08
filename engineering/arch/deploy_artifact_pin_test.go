package arch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// A deploy configuration pins exact artifact versions, while the plugin
// manifest is the version's only source of truth. When the two drift, nothing
// fails until something tries to resolve the reference: the seed loader rejects
// the whole platform, and a service baseline only fails on the first
// publication that uses it. Both are far from the edit that caused them, so the
// pins are reconciled here instead.
//
// The walk is deliberately shape-agnostic. Plugin references live in six
// different frontend profile fields plus backend services and baselines, and a
// typed check would silently stop covering any field added later.
func TestArch_DeployConfigurationPinsMatchPluginManifests(t *testing.T) {
	root := repoRoot(t)
	deployDir := filepath.Join(root, "engineering", "deploy")
	entries, err := os.ReadDir(deployDir)
	if err != nil {
		t.Fatal(err)
	}

	// Per-file coverage. A global count would still pass if one file's shape
	// changed and its references silently stopped being found, so every file
	// known to carry pins must keep yielding at least one.
	carriesPins := map[string]bool{
		"managed-services-profile.json":    false,
		"platform-management-profile.json": false,
		"portal-platform-catalog.json":     false,
	}
	for _, entry := range entries {
		name := entry.Name()
		// Example files intentionally reference artifacts that need not exist.
		if entry.IsDir() || !strings.HasSuffix(name, ".json") || strings.Contains(name, ".example.") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(deployDir, name))
		if err != nil {
			t.Fatal(err)
		}
		var document any
		if err := json.Unmarshal(raw, &document); err != nil {
			t.Fatalf("解析 engineering/deploy/%s: %v", name, err)
		}
		for _, reference := range collectPluginPins(document, "") {
			// Third-party artifacts have no in-repo manifest; only first-party
			// plugins carry a local version truth to reconcile against.
			manifestPath := filepath.Join(root, "extensions", "plugins", reference.id, "vastplan.plugin.json")
			manifestRaw, err := os.ReadFile(manifestPath)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			var manifest struct{ Version string }
			if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
				t.Fatalf("解析 %s 的 Manifest: %v", reference.id, err)
			}
			if _, tracked := carriesPins[name]; tracked {
				carriesPins[name] = true
			}
			if manifest.Version != reference.version {
				t.Errorf("部署配置钉的制品版本与本地 Manifest 不一致: engineering/deploy/%s%s\n  引用 %s@%s，Manifest 为 %s\n  原因: 插件 Manifest 是版本的唯一真相源，配置漂移只会在解析该引用时才失败",
					name, reference.path, reference.id, reference.version, manifest.Version)
			}
		}
	}
	for name, found := range carriesPins {
		if !found {
			t.Errorf("engineering/deploy/%s 不再产出任何第一方精确制品引用\n  原因: 该文件本应钉版本，走查失去覆盖说明其结构已改变，门禁对它已静默失效", name)
		}
	}
}

type pluginPin struct {
	id      string
	version string
	path    string
}

// collectPluginPins returns every {id, version} object reachable in the
// document, tagged with its JSON path so a failure names the exact reference.
func collectPluginPins(node any, path string) []pluginPin {
	switch value := node.(type) {
	case map[string]any:
		id, idOK := value["id"].(string)
		version, versionOK := value["version"].(string)
		if idOK && versionOK && strings.HasPrefix(id, "cn.vastplan.") {
			return []pluginPin{{id: id, version: version, path: path}}
		}
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var pins []pluginPin
		for _, key := range keys {
			pins = append(pins, collectPluginPins(value[key], path+"/"+key)...)
		}
		return pins
	case []any:
		var pins []pluginPin
		for index, item := range value {
			pins = append(pins, collectPluginPins(item, fmt.Sprintf("%s[%d]", path, index))...)
		}
		return pins
	default:
		return nil
	}
}
