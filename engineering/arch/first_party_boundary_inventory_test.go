package arch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
)

type firstPartyBoundaryInventory struct {
	Version             int `json:"version"`
	ProductCapabilities []struct {
		ID              string   `json:"id"`
		Artifacts       []string `json:"artifacts"`
		InternalModules []string `json:"internalModules"`
	} `json:"productCapabilities"`
	SignedLibraries []string `json:"signedLibraries"`
	RuntimePlugins  []string `json:"runtimePlugins"`
}

func TestFirstPartyBoundaryInventoryCoversEveryProductionArtifact(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "engineering", "governance", "first-party-plugin-boundaries.json"))
	if err != nil {
		t.Fatal(err)
	}
	var inventory firstPartyBoundaryInventory
	if err := json.Unmarshal(raw, &inventory); err != nil || inventory.Version != 1 {
		t.Fatalf("第一方插件边界清单无效: version=%d err=%v", inventory.Version, err)
	}

	classified := map[string]string{}
	for _, group := range []struct {
		name  string
		items []string
	}{{"runtimePlugins", inventory.RuntimePlugins}, {"signedLibraries", inventory.SignedLibraries}} {
		for _, id := range group.items {
			if previous := classified[id]; previous != "" {
				t.Errorf("第一方制品被重复分类: %s (%s, %s)", id, previous, group.name)
			}
			classified[id] = group.name
		}
	}

	entries, err := os.ReadDir(filepath.Join(root, "extensions", "plugins"))
	if err != nil {
		t.Fatal(err)
	}
	actual := map[string]struct{}{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, "extensions", "plugins", entry.Name(), "vastplan.plugin.json")); err == nil {
			actual[entry.Name()] = struct{}{}
		}
	}
	for id := range actual {
		if classified[id] == "" {
			t.Errorf("第一方生产制品缺少边界分类: %s", id)
		}
	}
	for id, class := range classified {
		if _, ok := actual[id]; !ok {
			t.Errorf("边界清单包含不存在的 %s: %s", class, id)
		}
	}
}

func TestDatabaseCapabilityPackKeepsProvidersAsInternalModules(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "engineering", "governance", "first-party-plugin-boundaries.json"))
	if err != nil {
		t.Fatal(err)
	}
	var inventory firstPartyBoundaryInventory
	if err := json.Unmarshal(raw, &inventory); err != nil {
		t.Fatal(err)
	}
	for _, capability := range inventory.ProductCapabilities {
		if capability.ID != "platform.database" {
			continue
		}
		artifacts := append([]string(nil), capability.Artifacts...)
		modules := append([]string(nil), capability.InternalModules...)
		sort.Strings(artifacts)
		sort.Strings(modules)
		wantArtifacts := []string{
			"cn.vastplan.foundation.data.relational.runtime",
			"cn.vastplan.platform.data.relational.connection-manager",
		}
		wantModules := []string{"mysql", "postgresql", "record-store", "sql-shared-state"}
		if !equalStrings(artifacts, wantArtifacts) || !equalStrings(modules, wantModules) {
			t.Fatalf("Database Capability Pack 边界漂移: artifacts=%v modules=%v", artifacts, modules)
		}
		for _, module := range modules {
			if _, err := os.Stat(filepath.Join(repoRoot(t), "extensions", "plugins", module, "vastplan.plugin.json")); err == nil {
				t.Errorf("Database Runtime 内部模块不得成为生产插件: %s", module)
			}
		}
		return
	}
	t.Fatal("缺少 platform.database 产品能力包")
}

func TestDatabaseCapabilityPackIsProjectedAsOneProductEntry(t *testing.T) {
	root := repoRoot(t)
	profile, err := backendcompositionv1.ParsePlatformProfileFile(filepath.Join(root, "engineering", "deploy", "platform-management-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range profile.ProductCapabilities {
		if capability.ID != "platform.database" {
			continue
		}
		artifacts := append([]string(nil), capability.Artifacts...)
		sort.Strings(artifacts)
		want := []string{
			"cn.vastplan.foundation.data.relational.runtime",
			"cn.vastplan.platform.data.relational.connection-manager",
		}
		if capability.EntryArtifact != "cn.vastplan.platform.data.relational.connection-manager" || !equalStrings(artifacts, want) {
			t.Fatalf("Database Capability Pack 产品投影漂移: entry=%s artifacts=%v", capability.EntryArtifact, artifacts)
		}
		for _, forbidden := range []string{"postgresql", "mysql", "record-store", "sql-shared-state"} {
			for _, artifact := range capability.Artifacts {
				if artifact == forbidden {
					t.Fatalf("内部模块不得成为产品可选制品: %s", forbidden)
				}
			}
		}
		return
	}
	t.Fatal("Platform Profile 缺少 platform.database 产品投影")
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
