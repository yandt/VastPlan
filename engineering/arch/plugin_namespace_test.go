package arch

import (
	"os"
	"path/filepath"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginid"
)

// 这些 ID 是多级分类规则采纳前的示例插件，只为兼容测试保留；新首方插件不得加入此表。
var legacyExamplePluginIDs = map[string]struct{}{
	"cn.vastplan.demo-audit":      {},
	"cn.vastplan.demo-permission": {},
	"cn.vastplan.demo-quota":      {},
	"cn.vastplan.hello-world":     {},
	"cn.vastplan.python-hello":    {},
}

func TestFirstPartyProductionPluginsUseClassifiableNamespaces(t *testing.T) {
	pluginsDir := filepath.Join(repoRoot(t), "extensions", "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(pluginsDir, entry.Name(), "vastplan.plugin.json"))
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := pluginv1.ParseManifest(raw)
		if err != nil {
			t.Fatalf("解析插件 %s: %v", entry.Name(), err)
		}
		if manifest.Publisher != "vastplan" {
			continue
		}
		if _, legacy := legacyExamplePluginIDs[manifest.ID]; legacy {
			continue
		}
		if _, err := pluginid.ParseFirstParty(manifest.ID); err != nil {
			t.Errorf("新增首方插件 %s 必须使用可分类多级命名空间: %v", manifest.ID, err)
		}
	}
}
