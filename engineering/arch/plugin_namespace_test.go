package arch

import (
	"os"
	"path/filepath"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginid"
)

func TestFirstPartyProductionPluginsUseProductionNamespaces(t *testing.T) {
	forEachPluginManifest(t, "extensions/plugins", func(path string, manifest pluginv1.Manifest) {
		if manifest.Publisher != pluginid.FirstPartyPublisher {
			return
		}
		if _, err := pluginid.ParseFirstParty(manifest.ID); err != nil {
			t.Errorf("生产首方插件 %s 必须使用可分类多级命名空间: %v", manifest.ID, err)
			return
		}
		class, err := pluginid.ClassifyManagement(manifest.ID, manifest.Publisher)
		if err != nil || class == pluginid.ManagementDevelopment {
			t.Errorf("开发插件不得进入 extensions/plugins: %s class=%s err=%v", path, class, err)
		}
	})
}

func TestExamplePluginsUseDevelopmentNamespaces(t *testing.T) {
	forEachPluginManifest(t, "examples/plugins", func(path string, manifest pluginv1.Manifest) {
		namespace, err := pluginid.ParseFirstParty(manifest.ID)
		if err != nil || namespace.Layer != pluginid.LayerExample {
			t.Errorf("示例插件必须使用 cn.vastplan.example.* 命名空间: %s err=%v", path, err)
			return
		}
		class, err := pluginid.ClassifyManagement(manifest.ID, manifest.Publisher)
		if err != nil || class != pluginid.ManagementDevelopment {
			t.Errorf("示例插件必须归类 development: %s class=%s err=%v", path, class, err)
		}
	})
}

func forEachPluginManifest(t *testing.T, relativeRoot string, visit func(string, pluginv1.Manifest)) {
	t.Helper()
	root := filepath.Join(repoRoot(t), filepath.FromSlash(relativeRoot))
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "vastplan.plugin.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("读取插件清单 %s: %v", path, err)
			continue
		}
		manifest, err := pluginv1.ParseManifest(raw)
		if err != nil {
			t.Errorf("解析插件清单 %s: %v", path, err)
			continue
		}
		if manifest.ID != entry.Name() {
			t.Errorf("插件目录与 ID 不一致: %s != %s", entry.Name(), manifest.ID)
			continue
		}
		visit(path, manifest)
	}
}
