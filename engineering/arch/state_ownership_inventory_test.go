package arch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type stateOwnershipInventory struct {
	Version        int `json:"version"`
	StateOwnership []struct {
		ID                string   `json:"id"`
		RuntimeStateModel string   `json:"runtimeStateModel"`
		DurableTruth      []string `json:"durableTruth"`
		TransientState    []string `json:"transientState"`
		LocalBoundaries   []string `json:"localBoundaries"`
	} `json:"stateOwnership"`
	RetiredDevelopmentStateFiles []struct {
		Path        string `json:"path"`
		Replacement string `json:"replacement"`
	} `json:"retiredDevelopmentStateFiles"`
}

type auditedPluginManifest struct {
	Runtime struct {
		StateModel string `json:"stateModel"`
	} `json:"runtime"`
	Capabilities struct {
		KernelServices []string `json:"kernelServices"`
	} `json:"capabilities"`
	Contributes struct {
		Backend struct {
			DataModels []json.RawMessage `json:"dataModels"`
		} `json:"backend"`
	} `json:"contributes"`
}

func TestFirstPartyStateOwnershipCoversEveryProductionPlugin(t *testing.T) {
	root := repoRoot(t)
	inventory := readStateOwnershipInventory(t, root)
	allowedDurable := stringSet("bootstrap-file", "browser-local", "platform-control-sql", "provider-private-file", "record-store", "shared-state")
	allowedTransient := stringSet("browser-memory", "browser-session", "process-memory", "provider-workspace")
	allowedBoundaries := stringSet("bootstrap-root", "derived-projection", "provider-private", "recovery-root")

	audited := map[string]struct{}{}
	previousID := ""
	for _, ownership := range inventory.StateOwnership {
		if ownership.ID == "" || ownership.ID <= previousID {
			t.Errorf("状态归属清单必须按唯一插件 ID 排序: previous=%q current=%q", previousID, ownership.ID)
		}
		previousID = ownership.ID
		if _, duplicate := audited[ownership.ID]; duplicate {
			t.Errorf("插件状态归属被重复声明: %s", ownership.ID)
		}
		audited[ownership.ID] = struct{}{}
		validateAuditedValues(t, ownership.ID, "durableTruth", ownership.DurableTruth, allowedDurable)
		validateAuditedValues(t, ownership.ID, "transientState", ownership.TransientState, allowedTransient)
		validateAuditedValues(t, ownership.ID, "localBoundaries", ownership.LocalBoundaries, allowedBoundaries)

		manifestPath := filepath.Join(root, "extensions", "plugins", ownership.ID, "vastplan.plugin.json")
		raw, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Errorf("状态归属清单包含不存在的插件 %s: %v", ownership.ID, err)
			continue
		}
		var manifest auditedPluginManifest
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Errorf("解析插件清单 %s: %v", ownership.ID, err)
			continue
		}
		actualStateModel := manifest.Runtime.StateModel
		if actualStateModel == "" {
			actualStateModel = "none"
		}
		if ownership.RuntimeStateModel != actualStateModel {
			t.Errorf("插件 %s 状态模型审计漂移: inventory=%s manifest=%s", ownership.ID, ownership.RuntimeStateModel, actualStateModel)
		}
		if containsString(ownership.DurableTruth, "shared-state") && !hasSharedStateKernelService(manifest.Capabilities.KernelServices) {
			t.Errorf("插件 %s 声明 Shared State 真相源但未申请 kernel.state.shared 服务", ownership.ID)
		}
		if containsString(ownership.DurableTruth, "record-store") && len(manifest.Contributes.Backend.DataModels) == 0 {
			t.Errorf("插件 %s 声明 Record Store 真相源但未贡献 DataModel", ownership.ID)
		}
		if containsString(ownership.DurableTruth, "bootstrap-file") && !containsString(ownership.LocalBoundaries, "bootstrap-root") {
			t.Errorf("插件 %s 的 Bootstrap 文件未归入 bootstrap-root", ownership.ID)
		}
		if containsString(ownership.DurableTruth, "provider-private-file") && !containsString(ownership.LocalBoundaries, "provider-private") {
			t.Errorf("插件 %s 的 Provider 文件未归入 provider-private", ownership.ID)
		}
	}

	entries, err := os.ReadDir(filepath.Join(root, "extensions", "plugins"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, "extensions", "plugins", entry.Name(), "vastplan.plugin.json")); err == nil {
			if _, ok := audited[entry.Name()]; !ok {
				t.Errorf("第一方生产插件缺少状态归属审计: %s", entry.Name())
			}
		}
	}
}

func TestRetiredDevelopmentStateFilesAreExplicitAndAbsentFromProductionPaths(t *testing.T) {
	root := repoRoot(t)
	inventory := readStateOwnershipInventory(t, root)
	want := []string{"state/api-exposure.json", "state/database-connections.json"}
	got := make([]string, 0, len(inventory.RetiredDevelopmentStateFiles))
	for _, item := range inventory.RetiredDevelopmentStateFiles {
		cleaned := filepath.ToSlash(filepath.Clean(item.Path))
		if cleaned != item.Path || filepath.IsAbs(item.Path) || strings.HasPrefix(cleaned, "../") || item.Replacement == "" {
			t.Errorf("退役状态文件声明无效: path=%q replacement=%q", item.Path, item.Replacement)
		}
		got = append(got, item.Path)
		basename := filepath.Base(item.Path)
		for _, relativeRoot := range []string{"core", "extensions/plugins", "engineering/deploy"} {
			assertProductionTreeDoesNotContain(t, filepath.Join(root, relativeRoot), basename)
		}
	}
	sort.Strings(got)
	if !equalStrings(got, want) {
		t.Fatalf("退役开发状态文件清单漂移: got=%v want=%v", got, want)
	}
}

func readStateOwnershipInventory(t *testing.T, root string) stateOwnershipInventory {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "engineering", "governance", "first-party-plugin-boundaries.json"))
	if err != nil {
		t.Fatal(err)
	}
	var inventory stateOwnershipInventory
	if err := json.Unmarshal(raw, &inventory); err != nil || inventory.Version != 1 {
		t.Fatalf("第一方插件状态归属清单无效: version=%d err=%v", inventory.Version, err)
	}
	return inventory
}

func validateAuditedValues(t *testing.T, pluginID, field string, values []string, allowed map[string]struct{}) {
	t.Helper()
	seen := map[string]struct{}{}
	for _, value := range values {
		if _, ok := allowed[value]; !ok {
			t.Errorf("插件 %s 的 %s 包含未知值 %q", pluginID, field, value)
		}
		if _, duplicate := seen[value]; duplicate {
			t.Errorf("插件 %s 的 %s 重复声明 %q", pluginID, field, value)
		}
		seen[value] = struct{}{}
	}
}

func assertProductionTreeDoesNotContain(t *testing.T, root, retiredBasename string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, ".test.ts") || strings.Contains(path, string(filepath.Separator)+"dist"+string(filepath.Separator)) {
			return nil
		}
		if filepath.Ext(path) != ".go" && filepath.Ext(path) != ".ts" && filepath.Ext(path) != ".json" && filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), retiredBasename) {
			t.Errorf("退役状态文件名重新进入生产路径: %s 包含 %s", path, retiredBasename)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func hasSharedStateKernelService(services []string) bool {
	for _, service := range services {
		if strings.HasPrefix(service, "kernel.state.shared.") {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
