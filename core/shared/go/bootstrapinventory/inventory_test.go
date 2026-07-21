package bootstrapinventory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestNormalizeRequiresExactLKGSubset(t *testing.T) {
	seed := Item{Ref: pluginv1.ArtifactRef{PluginID: "cn.vastplan.platform.artifacts.repository", Version: "1.0.0"}, SHA256: strings.Repeat("A", 64)}
	value, err := Normalize(Inventory{Version: Version, Generation: 7, RepositoryID: "primary", Seed: []Item{seed}, LastKnownGood: []Item{seed}})
	if err != nil {
		t.Fatal(err)
	}
	if value.Seed[0].Ref.Channel != "stable" || value.Seed[0].SHA256 != strings.Repeat("a", 64) || value.SeedSnapshot().OwnerID != "seed/primary" || value.LastKnownGoodSnapshot().OwnerID != "lkg/primary" {
		t.Fatalf("Bootstrap Inventory 未规范化: %+v", value)
	}
	other := seed
	other.Ref.Version = "2.0.0"
	if _, err := Normalize(Inventory{Version: Version, Generation: 8, RepositoryID: "primary", Seed: []Item{seed}, LastKnownGood: []Item{other}}); err == nil {
		t.Fatal("LKG 不得引用 Seed inventory 之外的制品")
	}
}

func TestParseFileRejectsBroadPermissionsAndSymlink(t *testing.T) {
	item := Item{Ref: pluginv1.ArtifactRef{PluginID: "cn.vastplan.platform.artifacts.repository", Version: "1.0.0"}, SHA256: strings.Repeat("a", 64)}
	raw, err := json.Marshal(Inventory{Version: Version, Generation: 1, RepositoryID: "primary", Seed: []Item{item}, LastKnownGood: []Item{item}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "inventory.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFile(path); err == nil {
		t.Fatal("other 可读的 Bootstrap Inventory 必须被拒绝")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFile(path); err != nil {
		t.Fatalf("owner-only Inventory 应可读取: %v", err)
	}
	link := filepath.Join(filepath.Dir(path), "inventory-link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFile(link); err == nil {
		t.Fatal("Bootstrap Inventory 符号链接必须被拒绝")
	}
}
