package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	workspace "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.versioning.workspace/versionworkspace"
)

func TestPluginVersionMatchesManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != pluginVersion || pluginVersion != workspace.PluginVersion {
		t.Fatalf("manifest=%q main=%q package=%q", manifest.Version, pluginVersion, workspace.PluginVersion)
	}
}
