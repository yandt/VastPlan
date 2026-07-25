package planner

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
)

func TestContributionRejectsUntrustedCallerAndTenantDrift(t *testing.T) {
	service, _ := New(Config{Channel: "stable", KernelVersion: "0.1.0", AllowedChannels: []string{"stable"}, AllowedPublishers: []string{"vastplan"}})
	handler := Contribution(service).Handlers[backendcompositionv1.PlanningOperation]
	if _, _, err := handler(context.Background(), nil, &contractv1.CallContext{TenantId: "acme", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "alice"}}, []byte(`{}`)); err == nil {
		t.Fatal("用户不得绕过 Deployment Manager 直接调用 Planner")
	}
	request := planningRequest()
	request.Intent.Metadata.Tenant = "other"
	raw, _ := json.Marshal(request)
	call := &contractv1.CallContext{TenantId: "acme", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: CallerID}}
	if _, _, err := handler(context.Background(), nil, call, raw); err == nil {
		t.Fatal("Planner 必须拒绝 Intent tenant 与可信上下文漂移")
	}
}

func TestManifestMatchesPlannerContribution(t *testing.T) {
	raw, err := os.ReadFile("../vastplan.plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ID != PluginID || manifest.Version != PluginVersion {
		t.Fatalf("Planner Manifest 与运行入口身份不一致: %s@%s", manifest.ID, manifest.Version)
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, contribution := range contributions {
		found = found || contribution.ExtensionPoint == extpoint.ToolPackage && contribution.ID == backendcompositionv1.PlanningCapability
	}
	if !found {
		t.Fatalf("Planner Manifest 未声明稳定 capability: %+v", contributions)
	}
}
