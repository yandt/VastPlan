package pluginv1

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInventoryProjectsUnknownContributionKindsDeterministically(t *testing.T) {
	manifest := Manifest{ID: "cn.vastplan.example", Version: "1.2.3", Publisher: "vastplan", Engines: map[string]string{"frontend": "^1.0"}, Entry: map[string]string{"frontend": "frontend/main.js"}, Contributes: map[string]json.RawMessage{
		"frontend": json.RawMessage(`{"rendererModules":[{"id":"antd","adapter":"ui.render.adapter","uiContract":"^9.0.0","engineFamily":"react","framework":"antd"}],"futureWidgets":[{"id":"future","contract":"^1.0.0","mode":"safe"}]}`),
	}}
	value := VerifiedArtifactManifest{Artifact: Artifact{PluginID: manifest.ID, Version: manifest.Version, Channel: "stable", SHA256: strings.Repeat("a", 64)}, Manifest: manifest}
	inventory, err := BuildPluginInventory(7, strings.Repeat("b", 64), []VerifiedArtifactManifest{value})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePluginInventory(inventory); err != nil {
		t.Fatal(err)
	}
	if len(inventory.Plugins) != 1 || !isSHA256(inventory.Plugins[0].InterfaceFingerprint) || len(inventory.Plugins[0].PublicInterface) == 0 {
		t.Fatalf("Inventory 未投影公共接口指纹: %+v", inventory.Plugins)
	}
	index, err := BuildContributionIndex(inventory, []VerifiedArtifactManifest{value})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateContributionIndex(index); err != nil {
		t.Fatal(err)
	}
	if len(index.Contributions) != 2 || index.Contributions[0].Kind != "frontend.futureWidgets" || index.Contributions[1].Kind != "frontend.rendererModules" {
		t.Fatalf("贡献索引未按通用 kind 投影: %+v", index.Contributions)
	}
	if index.Contributions[1].Owner.Ref.Version != "1.2.3" || index.Contributions[1].Contract != "^9.0.0" {
		t.Fatalf("精确所有者或契约丢失: %+v", index.Contributions[1])
	}
}

func TestInventoryRejectsArtifactManifestIdentityDrift(t *testing.T) {
	_, err := BuildPluginInventory(1, strings.Repeat("b", 64), []VerifiedArtifactManifest{{Artifact: Artifact{PluginID: "cn.vastplan.one", Version: "1.0.0", Channel: "stable", SHA256: strings.Repeat("a", 64)}, Manifest: Manifest{ID: "cn.vastplan.two", Version: "1.0.0", Publisher: "vastplan"}}})
	if err == nil {
		t.Fatal("应拒绝制品与 Manifest 身份漂移")
	}
}
