package pluginv1

import (
	"encoding/json"
	"strings"
	"testing"
)

type testReconciliationAdapter struct{ target string }

func (a testReconciliationAdapter) Target() string { return a.target }
func (a testReconciliationAdapter) Transition(value ReconciliationTransition) (string, error) {
	return a.target + "." + value.Operation, nil
}

func TestExplicitActivationClosesDependenciesAndPlansReplacement(t *testing.T) {
	values := []VerifiedArtifactManifest{
		inventoryFixture("cn.vastplan.app", "2.0.0", "stable", map[string]string{"cn.vastplan.library": "^1.0.0"}),
		inventoryFixture("cn.vastplan.library", "1.2.0", "stable", nil),
	}
	inventory, index := inventorySnapshots(t, values)
	selection, err := (ExplicitActivationPolicy{PolicyID: "profile-a", Kernel: PluginTargetBackend, Roots: []ArtifactRef{{PluginID: "cn.vastplan.app", Version: "2.0.0", Channel: "stable"}}}).Select(inventory, index)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Artifacts) != 2 {
		t.Fatalf("依赖闭包未进入选择: %+v", selection.Artifacts)
	}
	current := []PluginArtifactIdentity{{Ref: ArtifactRef{PluginID: "cn.vastplan.app", Version: "1.0.0", Channel: "stable"}, SHA256: strings.Repeat("c", 64), Publisher: "vastplan"}}
	plan, err := PlanReconciliation(selection, index, current, testReconciliationAdapter{target: PluginTargetBackend})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 2 || plan.Actions[0].Operation != ReconcileReplace || plan.Actions[1].Operation != ReconcileActivate {
		t.Fatalf("调和计划无效: %+v", plan.Actions)
	}
}

func TestDevelopmentActivationRejectsAmbiguousWorkspaceCandidates(t *testing.T) {
	values := []VerifiedArtifactManifest{inventoryFixture("cn.vastplan.app", "1.0.0-dev.workspace.a", "workspace", nil), inventoryFixture("cn.vastplan.app", "1.0.0-dev.workspace.b", "workspace", nil)}
	inventory, index := inventorySnapshots(t, values)
	if _, err := (DevelopmentActivationPolicy{PolicyID: "dev", Kernel: PluginTargetBackend}).Select(inventory, index); err == nil {
		t.Fatal("同 ID workspace 候选必须显式消歧")
	}
}

func inventoryFixture(id, version, channel string, dependencies map[string]string) VerifiedArtifactManifest {
	manifest := Manifest{ID: id, Version: version, Publisher: "vastplan", Engines: map[string]string{"backend": "^1.0"}, Entry: map[string]string{"backend": "bin/plugin"}, Dependencies: dependencies, Contributes: map[string]json.RawMessage{"backend": json.RawMessage(`{"tools":[{"id":"tool","service_role":"backend"}]}`)}}
	digestCharacter := "a"
	if strings.Contains(version, "workspace.b") {
		digestCharacter = "b"
	}
	return VerifiedArtifactManifest{Artifact: Artifact{PluginID: id, Version: version, Channel: channel, SHA256: strings.Repeat(digestCharacter, 64)}, Manifest: manifest}
}

func inventorySnapshots(t *testing.T, values []VerifiedArtifactManifest) (PluginInventorySnapshot, ContributionIndexSnapshot) {
	t.Helper()
	inventory, err := BuildPluginInventory(1, strings.Repeat("d", 64), values)
	if err != nil {
		t.Fatal(err)
	}
	index, err := BuildContributionIndex(inventory, values)
	if err != nil {
		t.Fatal(err)
	}
	return inventory, index
}
