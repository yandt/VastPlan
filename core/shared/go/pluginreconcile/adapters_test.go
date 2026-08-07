package pluginreconcile

import (
	"encoding/json"
	"strings"
	"testing"

	appv1 "cdsoft.com.cn/VastPlan/contracts/schemas/app/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestTargetAdaptersKeepRuntimeSemanticsSeparate(t *testing.T) {
	owner := pluginv1.PluginArtifactIdentity{Ref: pluginv1.ArtifactRef{PluginID: "cn.vastplan.renderer", Version: "1.0.0", Channel: "stable"}}
	index := pluginv1.ContributionIndexSnapshot{Contributions: []pluginv1.IndexedContribution{{Kind: "frontend.rendererModules", Owner: owner, Descriptor: json.RawMessage(`{"id":"antd"}`)}}}
	frontend, err := FrontendAdapter().Transition(pluginv1.ReconciliationTransition{Operation: pluginv1.ReconcileReplace, Candidate: &owner, Index: index})
	if err != nil || frontend != "frontend.host-epoch" {
		t.Fatalf("Renderer 必须使用 Host Epoch: %s %v", frontend, err)
	}
	backend, _ := BackendAdapter().Transition(pluginv1.ReconciliationTransition{Operation: pluginv1.ReconcileDeactivate, Current: &owner})
	desktop, _ := DesktopAdapter().Transition(pluginv1.ReconciliationTransition{Operation: pluginv1.ReconcileReplace, Candidate: &owner})
	mobile, _ := MobileAdapter().Transition(pluginv1.ReconciliationTransition{Operation: pluginv1.ReconcileReplace, Candidate: &owner})
	if backend != "backend.drain-stop" || desktop != "desktop.app-profile" || mobile != "mobile.bundle-publication" {
		t.Fatalf("各内核运行产物策略串线: %s %s %s", backend, desktop, mobile)
	}
}

func TestPlansMaterializeIntoEachKernelArtifact(t *testing.T) {
	backend := targetPlan(t, pluginv1.PluginTargetBackend, BackendAdapter())
	unit, err := ApplyBackendUnit(backend, deploymentv1.Unit{ID: "service", Kind: "service", Enabled: true, Replicas: 1})
	if err != nil || len(unit.Plugins) != 1 || unit.Plugins[0].SHA256 == "" {
		t.Fatalf("Backend Unit 投影失败: %+v %v", unit, err)
	}
	frontend := targetPlan(t, pluginv1.PluginTargetFrontend, FrontendAdapter())
	activation, err := BuildFrontendActivation(frontend)
	if err != nil || len(activation.Plugins) != 1 {
		t.Fatalf("Frontend Activation 投影失败: %+v %v", activation, err)
	}
	desktop := targetPlan(t, pluginv1.PluginTargetDesktop, DesktopAdapter())
	profile, err := ApplyDesktopProfile(desktop, appv1.Profile{Version: 1, ID: "desktop", TenantID: "tenant-a", Runtime: "desktop", Targets: []string{"linux/amd64"}, Distribution: "self-update", AssignedTo: []string{"desktop-a"}})
	if err != nil || len(profile.Plugins) != 1 {
		t.Fatalf("Desktop Profile 投影失败: %+v %v", profile, err)
	}
	mobile := targetPlan(t, pluginv1.PluginTargetMobile, MobileAdapter())
	bundle, err := BuildMobileBundle(mobile)
	if err != nil || !bundle.Republish || len(bundle.Plugins) != 1 {
		t.Fatalf("Mobile Bundle 投影失败: %+v %v", bundle, err)
	}
}

func targetPlan(t *testing.T, target string, adapter pluginv1.ReconciliationAdapter) pluginv1.ReconciliationPlan {
	t.Helper()
	manifest := pluginv1.Manifest{ID: "cn.vastplan.target", Version: "1.0.0", Publisher: "vastplan", Engines: map[string]string{target: "^1.0"}, Entry: map[string]string{target: "entry"}, Contributes: map[string]json.RawMessage{target: json.RawMessage(`{"views":[{"id":"view"}]}`)}}
	value := pluginv1.VerifiedArtifactManifest{Artifact: pluginv1.Artifact{PluginID: manifest.ID, Version: manifest.Version, Channel: "stable", SHA256: strings.Repeat("a", 64)}, Manifest: manifest}
	inventory, err := pluginv1.BuildPluginInventory(3, strings.Repeat("b", 64), []pluginv1.VerifiedArtifactManifest{value})
	if err != nil {
		t.Fatal(err)
	}
	index, err := pluginv1.BuildContributionIndex(inventory, []pluginv1.VerifiedArtifactManifest{value})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := (pluginv1.ExplicitActivationPolicy{PolicyID: "profile", Kernel: target, Roots: []pluginv1.ArtifactRef{{PluginID: manifest.ID, Version: manifest.Version, Channel: "stable"}}}).Select(inventory, index)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := pluginv1.PlanReconciliation(selection, index, nil, adapter)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
