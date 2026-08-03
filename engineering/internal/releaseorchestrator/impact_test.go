package releaseorchestrator

import (
	"encoding/json"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestAnalyzeReleaseImpactReusesCompatibleConsumersForImplementationAndAdditiveChange(t *testing.T) {
	workspace := impactWorkspace("2.1.0", "^2.0.0")
	for _, change := range []ReleaseChangeClass{ReleaseChangeImplementation, ReleaseChangeAdditive} {
		impacts, err := AnalyzeReleaseImpact(workspace, map[string]ReleasePluginRequest{"cn.vastplan.provider": {ID: "cn.vastplan.provider", Change: change}})
		if err != nil || len(impacts) != 1 || len(impacts[0].ReusedConsumers) != 1 || impacts[0].ReusedConsumers[0] != "cn.vastplan.consumer" {
			t.Fatalf("change=%s impacts=%+v err=%v", change, impacts, err)
		}
	}
}

func TestAnalyzeReleaseImpactRequiresConsumersForBreakingChange(t *testing.T) {
	workspace := impactWorkspace("2.1.0", "^2.0.0")
	_, err := AnalyzeReleaseImpact(workspace, map[string]ReleasePluginRequest{"cn.vastplan.provider": {ID: "cn.vastplan.provider", Change: ReleaseChangeBreaking}})
	if err == nil || !strings.Contains(err.Error(), "cn.vastplan.consumer") {
		t.Fatalf("breaking release must require direct consumer, err=%v", err)
	}
	impacts, err := AnalyzeReleaseImpact(workspace, map[string]ReleasePluginRequest{
		"cn.vastplan.provider": {ID: "cn.vastplan.provider", Change: ReleaseChangeBreaking},
		"cn.vastplan.consumer": {ID: "cn.vastplan.consumer", Change: ReleaseChangeImplementation},
	})
	providerImpact := impactFor(t, impacts, "cn.vastplan.provider")
	if err != nil || len(impacts) != 2 || len(providerImpact.RequiredConsumers) != 0 {
		t.Fatalf("selected consumer must close breaking release, impacts=%+v err=%v", impacts, err)
	}
}

func TestAnalyzeReleaseImpactWithBaselineRejectsChangedImplementation(t *testing.T) {
	baselineManifest := impactManifest("2.0.0", `{"views":[{"id":"home","route":"/"}]}`)
	workspace := impactWorkspaceWithManifest(impactManifest("2.1.0", `{"views":[{"id":"home","route":"/changed"}]}`), "^2.0.0")
	baseline := impactBaseline(t, baselineManifest)
	_, err := AnalyzeReleaseImpactWithBaseline(workspace, map[string]ReleasePluginRequest{
		"cn.vastplan.provider": {ID: "cn.vastplan.provider", Change: ReleaseChangeImplementation},
	}, baseline)
	if err == nil || !strings.Contains(err.Error(), "implementation") {
		t.Fatalf("changed implementation must be rejected, err=%v", err)
	}
}

func TestAnalyzeReleaseImpactWithBaselineAcceptsStructuralAdditiveChange(t *testing.T) {
	baselineManifest := impactManifest("2.0.0", `{"views":[{"id":"home","route":"/"}]}`)
	workspace := impactWorkspaceWithManifest(impactManifest("2.1.0", `{"views":[{"id":"home","route":"/"},{"id":"settings","route":"/settings"}]}`), "^2.0.0")
	impacts, err := AnalyzeReleaseImpactWithBaseline(workspace, map[string]ReleasePluginRequest{
		"cn.vastplan.provider": {ID: "cn.vastplan.provider", Change: ReleaseChangeAdditive},
	}, impactBaseline(t, baselineManifest))
	if err != nil {
		t.Fatal(err)
	}
	impact := impactFor(t, impacts, "cn.vastplan.provider")
	if impact.InterfaceChange != pluginv1.PublicInterfaceAdditive || impact.BaselineInterfaceFingerprint == "" {
		t.Fatalf("additive impact=%+v", impact)
	}
}

func TestAnalyzeReleaseImpactWithBaselineRejectsNonMonotonicAdditiveChange(t *testing.T) {
	baselineManifest := impactManifest("2.0.0", `{"views":[{"id":"home","route":"/"}]}`)
	workspace := impactWorkspaceWithManifest(impactManifest("2.1.0", `{"views":[{"id":"home","route":"/changed"}]}`), "^2.0.0")
	_, err := AnalyzeReleaseImpactWithBaseline(workspace, map[string]ReleasePluginRequest{
		"cn.vastplan.provider": {ID: "cn.vastplan.provider", Change: ReleaseChangeAdditive},
	}, impactBaseline(t, baselineManifest))
	if err == nil || !strings.Contains(err.Error(), "additive") {
		t.Fatalf("non-monotonic additive change must be rejected, err=%v", err)
	}
}

func impactFor(t *testing.T, impacts []ReleaseImpact, pluginID string) ReleaseImpact {
	t.Helper()
	for _, impact := range impacts {
		if impact.PluginID == pluginID {
			return impact
		}
	}
	t.Fatalf("missing impact for %s: %+v", pluginID, impacts)
	return ReleaseImpact{}
}

func impactWorkspace(providerVersion, consumerConstraint string) PluginWorkspace {
	return impactWorkspaceWithManifest(pluginv1.Manifest{ID: "cn.vastplan.provider", Version: providerVersion}, consumerConstraint)
}

func impactWorkspaceWithManifest(providerManifest pluginv1.Manifest, consumerConstraint string) PluginWorkspace {
	provider := WorkspacePlugin{ID: providerManifest.ID, Version: providerManifest.Version, Manifest: providerManifest}
	consumer := WorkspacePlugin{ID: "cn.vastplan.consumer", Version: "1.0.0", Manifest: pluginv1.Manifest{ID: "cn.vastplan.consumer", Version: "1.0.0", Dependencies: map[string]string{"cn.vastplan.provider": consumerConstraint}}}
	return PluginWorkspace{Plugins: map[string]WorkspacePlugin{provider.ID: provider, consumer.ID: consumer}}
}

func impactManifest(version, views string) pluginv1.Manifest {
	return pluginv1.Manifest{
		ID: "cn.vastplan.provider", Version: version, Publisher: "vastplan",
		Contributes: map[string]json.RawMessage{"frontend": json.RawMessage(views)},
	}
}

func impactBaseline(t *testing.T, manifest pluginv1.Manifest) *InterfaceBaseline {
	t.Helper()
	inventory, err := pluginv1.BuildPluginInventory(1, strings.Repeat("b", 64), []pluginv1.VerifiedArtifactManifest{{
		Artifact: pluginv1.Artifact{PluginID: manifest.ID, Version: manifest.Version, Channel: "stable", SHA256: strings.Repeat("a", 64)}, Manifest: manifest,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return &InterfaceBaseline{Source: "test", Inventory: inventory}
}
