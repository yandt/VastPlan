package arch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type frontendPackage struct {
	Name            string            `json:"name"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

// Frontend 插件可以依赖 SDK 和外部库，但不得把另一个插件的源码包当作
// workspace 依赖。插件之间的运行关系只能由签名 Manifest、Catalog 和稳定
// 前端契约表达；否则一个看似独立的制品会在构建时偷偷内嵌另一个插件。
func TestFrontendPluginsDoNotDependOnOtherFrontendPlugins(t *testing.T) {
	pluginsDir := filepath.Join(repoRoot(t), "extensions", "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		t.Fatal(err)
	}

	type foundPackage struct {
		pluginID string
		path     string
		manifest frontendPackage
	}
	packages := make([]foundPackage, 0, len(entries))
	owners := make(map[string]string)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(pluginsDir, entry.Name(), "frontend", "package.json")
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("读取 %s: %v", path, err)
		}
		var manifest frontendPackage
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Fatalf("解析 %s: %v", path, err)
		}
		if manifest.Name == "" {
			t.Fatalf("%s 缺少 package name", path)
		}
		if previous, exists := owners[manifest.Name]; exists {
			t.Fatalf("前端 workspace package 重名 %s: %s / %s", manifest.Name, previous, entry.Name())
		}
		owners[manifest.Name] = entry.Name()
		packages = append(packages, foundPackage{pluginID: entry.Name(), path: path, manifest: manifest})
	}

	for _, current := range packages {
		for section, dependencies := range map[string]map[string]string{
			"dependencies":    current.manifest.Dependencies,
			"devDependencies": current.manifest.DevDependencies,
		} {
			for dependency := range dependencies {
				owner, isPluginPackage := owners[dependency]
				if !isPluginPackage || owner == current.pluginID {
					continue
				}
				t.Errorf("前端插件源码依赖越界：%s 的 %s 依赖插件 %s (%s)\n  原因: 插件只依赖 SDK/契约；其他插件必须通过 Manifest Catalog 在运行时组合",
					current.pluginID, section, owner, dependency)
			}
		}
	}
}
