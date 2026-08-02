package main

import (
	"os"
	"path/filepath"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestPluginVersionMatchesManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ID != pluginID || manifest.Version != pluginVersion {
		t.Fatalf("插件版本投影不一致: manifest=%s@%s binary=%s@%s", manifest.ID, manifest.Version, pluginID, pluginVersion)
	}
}
