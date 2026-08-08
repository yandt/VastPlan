package arch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	sharedstatev1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstate/v1"
)

type stateOwnershipInventory struct {
	Version                      int                    `json:"version"`
	StateOwnership               []stateOwnershipRecord `json:"stateOwnership"`
	RetiredDevelopmentStateFiles []struct {
		Path        string `json:"path"`
		Replacement string `json:"replacement"`
	} `json:"retiredDevelopmentStateFiles"`
}

// stateOwnershipRecord is shared by inventory validation and architecture
// gates. Runtime topology remains recorded here, while direct and delegated
// durable truth drive storage ownership checks.
type stateOwnershipRecord struct {
	ID                    string                          `json:"id"`
	RuntimeStateModel     string                          `json:"runtimeStateModel"`
	DurableTruth          []string                        `json:"durableTruth"`
	DelegatedDurableTruth []stateOwnershipTruthDelegation `json:"delegatedDurableTruth"`
	TransientState        []string                        `json:"transientState"`
	LocalBoundaries       []string                        `json:"localBoundaries"`
}

type stateOwnershipTruthDelegation struct {
	Capability     string `json:"capability"`
	LogicalService string `json:"logicalService"`
}

type auditedRuntimeCapability struct {
	Capability string `json:"capability"`
}

type auditedRuntimeRequirement struct {
	Capability     string `json:"capability"`
	Scope          string `json:"scope"`
	Kind           string `json:"kind"`
	LogicalService string `json:"logicalService"`
}

type auditedPluginManifest struct {
	Runtime struct {
		StateModel string                      `json:"stateModel"`
		Provides   []auditedRuntimeCapability  `json:"provides"`
		Requires   []auditedRuntimeRequirement `json:"requires"`
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
	ownershipByID := make(map[string]stateOwnershipRecord, len(inventory.StateOwnership))
	manifestsByID := make(map[string]auditedPluginManifest, len(inventory.StateOwnership))
	providersByCapability := map[string][]string{}
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
		ownershipByID[ownership.ID] = ownership
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
		manifestsByID[ownership.ID] = manifest
		for _, provided := range manifest.Runtime.Provides {
			providersByCapability[provided.Capability] = append(providersByCapability[provided.Capability], ownership.ID)
		}
	}

	delegationEdges := map[string][]string{}
	for _, ownership := range inventory.StateOwnership {
		manifest, found := manifestsByID[ownership.ID]
		if !found {
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
		if hasSharedStateKernelService(manifest.Capabilities.KernelServices) && !containsString(ownership.DurableTruth, "shared-state") {
			t.Errorf("插件 %s 申请 kernel.state.shared 服务但未声明 Shared State 真相源", ownership.ID)
		}
		hasRecordStore := hasStrongRemoteRequirement(manifest, recordstorev1.Capability, "")
		if containsString(ownership.DurableTruth, "record-store") && (len(manifest.Contributes.Backend.DataModels) == 0 || !hasRecordStore) {
			t.Errorf("插件 %s 声明 Record Store 真相源但未同时贡献 DataModel 和申请强远程 Record Store", ownership.ID)
		}
		if hasRecordStore && !containsString(ownership.DurableTruth, "record-store") {
			t.Errorf("插件 %s 申请强远程 Record Store 但未声明 Record Store 真相源", ownership.ID)
		}
		if containsString(ownership.DurableTruth, "bootstrap-file") && !containsString(ownership.LocalBoundaries, "bootstrap-root") {
			t.Errorf("插件 %s 的 Bootstrap 文件未归入 bootstrap-root", ownership.ID)
		}
		if containsString(ownership.DurableTruth, "provider-private-file") && !containsString(ownership.LocalBoundaries, "provider-private") {
			t.Errorf("插件 %s 的 Provider 文件未归入 provider-private", ownership.ID)
		}
		validateDurableTruthDelegations(t, ownership, manifest, providersByCapability, delegationEdges)
	}
	validateDelegatedDurableTruthTerminates(t, ownershipByID, delegationEdges)

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
		if strings.HasPrefix(service, sharedstatev1.KernelServicePrefix) {
			return true
		}
	}
	return false
}

func hasStrongRemoteRequirement(manifest auditedPluginManifest, capability, logicalService string) bool {
	for _, requirement := range manifest.Runtime.Requires {
		if requirement.Capability == capability && requirement.Scope == "remote" && requirement.Kind == "strong" &&
			(logicalService == "" || requirement.LogicalService == logicalService) {
			return true
		}
	}
	return false
}

func TestStrongRemoteRequirementMatchesDelegatedTruth(t *testing.T) {
	manifest := auditedPluginManifest{}
	manifest.Runtime.Requires = []auditedRuntimeRequirement{{
		Capability: "platform.portal-composer", Scope: "remote", Kind: "strong", LogicalService: "platform.portal-composer",
	}}
	if !hasStrongRemoteRequirement(manifest, "platform.portal-composer", "platform.portal-composer") {
		t.Fatal("匹配的 strong remote requirement 应被识别")
	}
	if hasStrongRemoteRequirement(manifest, "platform.portal-composer", "other-service") {
		t.Fatal("不同 logical service 不得满足耐久真相委托")
	}
	manifest.Runtime.Requires[0].Kind = "lazy"
	if hasStrongRemoteRequirement(manifest, "platform.portal-composer", "platform.portal-composer") {
		t.Fatal("lazy requirement 不得作为耐久真相委托")
	}
}

func validateDurableTruthDelegations(t *testing.T, ownership stateOwnershipRecord, manifest auditedPluginManifest, providersByCapability map[string][]string, edges map[string][]string) {
	t.Helper()
	previous := ""
	for _, delegation := range ownership.DelegatedDurableTruth {
		key := delegation.Capability + "\x00" + delegation.LogicalService
		if delegation.Capability == "" || delegation.LogicalService == "" || key <= previous {
			t.Errorf("插件 %s 的 delegatedDurableTruth 必须完整、唯一并按 capability/logicalService 排序", ownership.ID)
		}
		previous = key
		if !hasStrongRemoteRequirement(manifest, delegation.Capability, delegation.LogicalService) {
			t.Errorf("插件 %s 委托耐久真相给 %s 但未声明匹配的 strong remote requirement", ownership.ID, delegation.Capability)
		}
		providers := providersByCapability[delegation.Capability]
		if len(providers) == 0 {
			t.Errorf("插件 %s 委托耐久真相给无第一方提供者的能力 %s", ownership.ID, delegation.Capability)
			continue
		}
		edges[ownership.ID] = append(edges[ownership.ID], providers...)
	}
}

func validateDelegatedDurableTruthTerminates(t *testing.T, ownershipByID map[string]stateOwnershipRecord, edges map[string][]string) {
	t.Helper()
	for source, targets := range edges {
		for _, target := range targets {
			if err := delegatedTruthReachesOwner(target, ownershipByID, edges, map[string]bool{source: true}); err != nil {
				t.Errorf("插件 %s 的耐久真相委托无效: %v", source, err)
			}
		}
	}
}

func TestDelegatedDurableTruthMustTerminateAtOwnedTruth(t *testing.T) {
	ownership := map[string]stateOwnershipRecord{
		"delegate": {ID: "delegate"},
		"middle":   {ID: "middle"},
		"owner":    {ID: "owner", DurableTruth: []string{"shared-state"}},
	}
	tests := []struct {
		name  string
		start string
		edges map[string][]string
		want  string
	}{
		{name: "terminal owner", start: "middle", edges: map[string][]string{"middle": {"owner"}}},
		{name: "missing terminal owner", start: "middle", edges: map[string][]string{}, want: "未落到耐久真相拥有者"},
		{name: "cycle", start: "middle", edges: map[string][]string{"middle": {"delegate"}, "delegate": {"middle"}}, want: "委托链形成环"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := delegatedTruthReachesOwner(test.start, ownership, test.edges, map[string]bool{})
			if test.want == "" && err != nil {
				t.Fatal(err)
			}
			if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("err=%v, want containing %q", err, test.want)
			}
		})
	}
}

func delegatedTruthReachesOwner(pluginID string, ownershipByID map[string]stateOwnershipRecord, edges map[string][]string, visiting map[string]bool) error {
	if visiting[pluginID] {
		return fmt.Errorf("委托链形成环: %s", pluginID)
	}
	ownership, found := ownershipByID[pluginID]
	if !found {
		return fmt.Errorf("委托目标未进入状态归属清单: %s", pluginID)
	}
	if len(ownership.DurableTruth) > 0 {
		return nil
	}
	targets := edges[pluginID]
	if len(targets) == 0 {
		return fmt.Errorf("委托链未落到耐久真相拥有者: %s", pluginID)
	}
	visiting[pluginID] = true
	defer delete(visiting, pluginID)
	for _, target := range targets {
		if err := delegatedTruthReachesOwner(target, ownershipByID, edges, visiting); err != nil {
			return err
		}
	}
	return nil
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
