package enforcer

import (
	"encoding/json"
	"os"
	"testing"
)

func TestPluginIdentityMatchesSignedManifest(t *testing.T) {
	raw, err := os.ReadFile("../vastplan.plugin.json")
	if err != nil {
		t.Fatalf("读取插件清单: %v", err)
	}
	var manifest struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("解析插件清单: %v", err)
	}
	if manifest.ID != PluginID || manifest.Version != PluginVersion {
		t.Fatalf("运行时身份与签名清单不一致: runtime=%s@%s manifest=%s@%s", PluginID, PluginVersion, manifest.ID, manifest.Version)
	}
}
