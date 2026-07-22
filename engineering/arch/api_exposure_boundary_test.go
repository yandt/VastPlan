package arch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// api.route 仍属于 Backend v1 兼容面，但产品插件必须使用治理式 apiContracts。
// 这条门禁防止旧公开路径模型重新进入 Node Gateway。
func TestProductPluginsDoNotUseDeprecatedAPIRoutes(t *testing.T) {
	pluginsDir := filepath.Join("..", "..", "extensions", "plugins")
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
		var manifest struct {
			Contributes struct {
				Backend map[string]json.RawMessage `json:"backend"`
			} `json:"contributes"`
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Fatalf("解析 %s: %v", entry.Name(), err)
		}
		if _, deprecated := manifest.Contributes.Backend["apiRoutes"]; deprecated {
			t.Errorf("产品插件 %s 使用已废弃 apiRoutes；应声明 apiContracts 并由 ApiExposure 发布", entry.Name())
		}
	}
}
