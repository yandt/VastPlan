package releaseorchestrator

import (
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
	provider := WorkspacePlugin{ID: "cn.vastplan.provider", Version: providerVersion, Manifest: pluginv1.Manifest{ID: "cn.vastplan.provider", Version: providerVersion}}
	consumer := WorkspacePlugin{ID: "cn.vastplan.consumer", Version: "1.0.0", Manifest: pluginv1.Manifest{ID: "cn.vastplan.consumer", Version: "1.0.0", Dependencies: map[string]string{"cn.vastplan.provider": consumerConstraint}}}
	return PluginWorkspace{Plugins: map[string]WorkspacePlugin{provider.ID: provider, consumer.ID: consumer}}
}
