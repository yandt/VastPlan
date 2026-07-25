//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"sort"
	"strings"
	"testing"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/catalog"
	plannerplugin "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.infrastructure.composition-planner/planner"
)

func TestApplicationIntentP5_PlannerUsesSignedRealArtifactClosure(t *testing.T) {
	repository := newP5FixtureRepository(t)
	planner := newP5Planner(t)
	request := p5PlanningRequest([]string{"audit"}, p5PlatformProfile())

	missing, err := planner.Plan(context.Background(), repository, request)
	if err != nil {
		t.Fatal(err)
	}
	if missing.Status != backendcompositionv1.ResolutionNeedsConfiguration || missing.ApplicationComposition == nil || missing.ArtifactLock == nil {
		t.Fatalf("真实插件配置缺口未形成可解释方案: %+v", missing)
	}
	if len(missing.Features) != 1 || missing.Features[0].FeatureID != "audit" {
		t.Fatalf("真实 Feature 未进入解析证据: %+v", missing.Features)
	}
	want := []string{p5AuditID, p5CommonID, p5QuotaID, p5RootID}
	if got := lockPluginIDs(*missing.ArtifactLock); !equalStrings(got, want) {
		t.Fatalf("Feature/传递依赖闭包错误: got=%v want=%v lock=%+v", got, want, missing.ArtifactLock)
	}
	rootPlan := configurationPlanItem(missing, p5RootID)
	if rootPlan == nil || len(rootPlan.Missing) != 1 || rootPlan.Missing[0].Kind != "managed-credential" || rootPlan.Missing[0].Field != "token" {
		t.Fatalf("真实插件托管凭证缺口不精确: %+v", missing.ConfigurationPlan)
	}

	request.ConfigurationSnapshot = p5CredentialSnapshot()
	resolved, err := planner.Plan(context.Background(), repository, request)
	if err != nil || resolved.Status != backendcompositionv1.ResolutionResolved || resolved.PlanDigest == "" {
		t.Fatalf("可信 CredentialRef 没有使真实计划收敛: status=%s err=%v report=%+v", resolved.Status, err, resolved)
	}
	if raw := string(mustJSON(t, resolved)); strings.Contains(raw, "secret-material") || !strings.Contains(raw, "credential://managed/p5-pipeline") {
		t.Fatalf("Resolution Report 凭证投影错误: %s", raw)
	}
}

func TestApplicationIntentP5_PlannerExplainsVersionConflictAndProviderAmbiguity(t *testing.T) {
	repository := newP5FixtureRepository(t)
	planner := newP5Planner(t)

	_, err := planner.Plan(context.Background(), repository, p5PlanningRequest([]string{"conflict"}, p5PlatformProfile()))
	var resolutionErr *catalog.ResolutionError
	if !errors.As(err, &resolutionErr) || resolutionErr.Code != "VERSION_CONFLICT" || !strings.Contains(resolutionErr.Message, p5CommonID) {
		t.Fatalf("真实制品版本冲突没有保留仓库稳定语义: typed=%+v err=%v", resolutionErr, err)
	}

	report, err := planner.Plan(context.Background(), repository, p5PlanningRequest([]string{"provider"}, p5AmbiguousProviderProfile()))
	if err != nil || report.Status != backendcompositionv1.ResolutionInvalid || len(report.Diagnostics) != 1 || !strings.Contains(report.Diagnostics[0].Message, "候选 Provider") {
		t.Fatalf("Provider 歧义必须形成可解释 Invalid report: report=%+v err=%v", report, err)
	}
}

func newP5Planner(t *testing.T) *plannerplugin.Service {
	t.Helper()
	service, err := plannerplugin.New(plannerplugin.Config{
		Channel: "testing", KernelVersion: "0.1.0", Platform: runtime.GOOS + "/" + runtime.GOARCH,
		AllowedChannels: []string{p5Channel}, AllowedPublishers: []string{"vastplan"},
		AllowedPluginPrefixes: []string{"cn.vastplan.fixture"}, AllowDevelopmentPlugins: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func p5PlanningRequest(features []string, profile backendcompositionv1.PlatformProfile) backendcompositionv1.PlanningRequest {
	intent := backendcompositionv1.ApplicationIntent{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "p5-pipeline"},
		Target:   compositioncommonv1.Target{Kernel: compositioncommonv1.KernelBackend},
		Metadata: deploymentv1.Metadata{Name: "p5-pipeline", Tenant: "acme"},
		Services: []backendcompositionv1.ServiceIntent{{
			ID: "pipeline", ServiceClass: "application.backend",
			RootPlugins: []backendcompositionv1.RootPluginSelection{{
				Ref: pluginv1.ArtifactRef{PluginID: p5RootID, Version: "1.0.0", Channel: p5Channel}, Features: append([]string(nil), features...),
			}},
			PluginConfig: map[string]map[string]any{p5RootID: {"endpoint": "https://pipeline.example", "audit_mode": true}},
			Operations:   backendcompositionv1.ServiceOperationsIntent{Replicas: 1},
		}},
	}
	intent, _ = backendcompositionv1.ValidateApplicationIntent(intent)
	return backendcompositionv1.PlanningRequest{Intent: intent, PlatformProfile: profile}
}

func p5PlatformProfile() backendcompositionv1.PlatformProfile {
	profile := backendcompositionv1.PlatformProfile{
		Document:       compositioncommonv1.Document{Version: 1, Revision: 1, ID: "p5-backend"},
		Target:         compositioncommonv1.Target{Kernel: compositioncommonv1.KernelBackend},
		ServiceClasses: []string{"application.backend"}, ServiceBaselines: []backendcompositionv1.ServiceBaseline{}, Services: []deploymentv2.ServiceUnit{},
	}
	profile, _ = backendcompositionv1.ValidatePlatformProfile(profile)
	return profile
}

func p5AmbiguousProviderProfile() backendcompositionv1.PlatformProfile {
	profile := p5PlatformProfile()
	profile.Services = []deploymentv2.ServiceUnit{
		p5ProviderUnit("settings-a", "fixture.settings.a", p5ProviderAID),
		p5ProviderUnit("settings-b", "fixture.settings.b", p5ProviderBID),
	}
	profile, _ = backendcompositionv1.ValidatePlatformProfile(profile)
	return profile
}

func p5ProviderUnit(id, logicalService, pluginID string) deploymentv2.ServiceUnit {
	return deploymentv2.ServiceUnit{
		ID: id, Kind: "service", Enabled: true, ServiceRole: "backend", LogicalService: logicalService,
		InstancePolicy: "active-active", StateModel: "external-shared", Visibility: "cluster", Routing: "queue", RoutingDomain: "application", Replicas: 1,
		Plugins: []deploymentv1.PluginRef{{ID: pluginID, Version: "1.0.0", Channel: p5Channel}},
	}
}

func p5CredentialSnapshot() *backendcompositionv1.PlanningConfigurationSnapshot {
	snapshot := backendcompositionv1.PlanningConfigurationSnapshot{Version: 1, Bindings: []backendcompositionv1.PlanningCredentialBinding{{
		UnitID: "pipeline", PluginID: p5RootID, FieldID: "token",
		Ref: commonv1.ManagedCredentialRef{Handle: "credential://managed/p5-pipeline", Scope: "tenant", Owner: p5RootID, Purpose: "fixture.composition.root", Version: 1},
	}}}
	snapshot.Digest = snapshot.ComputedDigest()
	return &snapshot
}

func lockPluginIDs(lock pluginv1.ArtifactLock) []string {
	values := make([]string, 0, len(lock.Packages))
	for _, item := range lock.Packages {
		values = append(values, item.Ref.PluginID)
	}
	sort.Strings(values)
	return values
}

func configurationPlanItem(report backendcompositionv1.ResolutionReport, pluginID string) *backendcompositionv1.ConfigurationPlanItem {
	for index := range report.ConfigurationPlan.Items {
		if report.ConfigurationPlan.Items[index].PluginID == pluginID {
			return &report.ConfigurationPlan.Items[index]
		}
	}
	return nil
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

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
