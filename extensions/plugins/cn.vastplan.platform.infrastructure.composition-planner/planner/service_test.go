package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type planningRepository struct {
	descriptors map[string]pluginv1.ArtifactPlanningDescriptor
	lastResolve pluginv1.ArtifactResolveRequest
}

func (r *planningRepository) Describe(_ context.Context, request pluginv1.ArtifactPlanningRequest) (pluginv1.ArtifactPlanningResponse, error) {
	var items []pluginv1.ArtifactPlanningDescriptor
	for _, ref := range request.Refs {
		item, ok := r.descriptors[ref.PluginID]
		if !ok || item.Ref != ref {
			return pluginv1.ArtifactPlanningResponse{}, fmt.Errorf("not found: %+v", ref)
		}
		items = append(items, item)
	}
	return pluginv1.ValidateArtifactPlanningResponse(pluginv1.ArtifactPlanningResponse{RepositoryRevision: 9, Items: items})
}

func (r *planningRepository) Resolve(_ context.Context, request pluginv1.ArtifactResolveRequest) (pluginv1.ArtifactLock, error) {
	r.lastResolve = request
	root := r.descriptors["cn.vastplan.product.agent.api"]
	audit := r.descriptors["cn.vastplan.product.agent.audit"]
	packages := []pluginv1.ArtifactLockPackage{
		{Ref: audit.Ref, SHA256: audit.SHA256, Size: 100, Publisher: audit.Publisher, KeyID: "release", RepositoryRevision: 8},
		{Ref: root.Ref, SHA256: root.SHA256, Size: 100, Publisher: root.Publisher, KeyID: "release", RepositoryRevision: 7},
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].Ref.PluginID < packages[j].Ref.PluginID })
	lock := pluginv1.ArtifactLock{
		SchemaVersion: "v1", RepositoryRevision: 9, Target: request.Target, KernelVersion: request.KernelVersion,
		Platform: request.Platform, Roots: append([]pluginv1.ArtifactRequirement(nil), request.Roots...), Packages: packages,
	}
	lock.Digest, _ = pluginv1.ArtifactLockDigest(lock)
	return lock, nil
}

func TestPlannerCompilesFeatureProviderGraphAndConfigurationPlan(t *testing.T) {
	repository := plannerFixture(t, "active-active")
	service, err := New(Config{
		Channel: "stable", KernelVersion: "0.1.0", Platform: "linux/amd64", AllowedChannels: []string{"stable"},
		AllowedPublishers: []string{"vastplan"}, AllowedPluginPrefixes: []string{"cn.vastplan"},
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := service.Plan(context.Background(), repository, planningRequest())
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != backendcompositionv1.ResolutionNeedsConfiguration || report.ApplicationComposition == nil || report.ArtifactLock == nil {
		t.Fatalf("Planner 未生成可配置方案: %+v", report)
	}
	unit := report.ApplicationComposition.Units[0].Spec
	if len(unit.Plugins) != 2 || unit.Plugins[0].ID != "cn.vastplan.product.agent.api" || unit.Plugins[1].ID != "cn.vastplan.product.agent.audit" {
		t.Fatalf("Feature 包依赖闭包错误: %+v", unit.Plugins)
	}
	if len(report.Features) != 1 || report.Features[0].FeatureID != "audit" {
		t.Fatalf("Feature 解释缺失: %+v", report.Features)
	}
	if len(report.ProviderBindings) != 1 || report.ProviderBindings[0].ProviderUnitID != "settings" || len(report.ServiceGraph.Edges) != 1 {
		t.Fatalf("Provider Binding 或 Service DAG 错误: bindings=%+v graph=%+v", report.ProviderBindings, report.ServiceGraph)
	}
	if len(unit.DependsOn) != 0 {
		t.Fatalf("Platform 外部服务不能被写进独立 Application Composition 的 depends_on: %+v", unit.DependsOn)
	}
	if len(report.ConfigurationPlan.Items) != 1 || len(report.ConfigurationPlan.Items[0].Missing) != 1 || report.ConfigurationPlan.Items[0].Missing[0].Kind != "managed-credential" {
		t.Fatalf("配置计划没有精确报告缺失托管凭证: %+v", report.ConfigurationPlan)
	}
	if len(report.PlanDigest) != 64 || report.Planner.ConfigurationDigest != service.config.Digest() {
		t.Fatalf("Planner 身份或方案摘要未绑定: %+v", report.Planner)
	}
	if len(repository.lastResolve.AvailableCapabilities) != 1 || repository.lastResolve.AvailableCapabilities[0].Capability != "platform.settings" {
		t.Fatalf("Platform Profile Provider 未投影到仓库求解: %+v", repository.lastResolve.AvailableCapabilities)
	}
	for _, root := range repository.lastResolve.Roots {
		if root.PluginID == "cn.vastplan.product.agent.api" && (root.Channel != "stable" || !slices.Equal(root.Features, []string{"audit"})) {
			t.Fatalf("Intent 根插件的 channel/Feature 未进入仓库锁约束: %+v", root)
		}
	}
}

func TestResolveRequirementsIntersectsVersionsUnionsFeaturesAndRejectsChannels(t *testing.T) {
	intent := backendcompositionv1.ApplicationIntent{Services: []backendcompositionv1.ServiceIntent{
		{ID: "api", RootPlugins: []pluginv1.ArtifactRequirement{{PluginID: "cn.example.app", Constraint: "^1.0.0", Channel: "stable", Features: []string{"audit"}}}},
		{ID: "worker", RootPlugins: []pluginv1.ArtifactRequirement{{PluginID: "cn.example.app", Constraint: "^1.2.0", Channel: "stable", Features: []string{"trace"}}}},
	}}
	roots, err := resolveRequirements(intent, backendcompositionv1.PlatformProfile{})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0].Constraint != "^1.0.0, ^1.2.0" || !slices.Equal(roots[0].Features, []string{"audit", "trace"}) {
		t.Fatalf("多服务 Requirement 聚合错误: %+v", roots)
	}
	intent.Services[1].RootPlugins[0].Channel = "testing"
	if _, err := resolveRequirements(intent, backendcompositionv1.PlatformProfile{}); err == nil || !strings.Contains(err.Error(), "冲突 channel") {
		t.Fatalf("渠道冲突必须显式拒绝: %v", err)
	}
}

func TestPlannerReturnsInvalidReportForIncompatibleCoLocatedPolicies(t *testing.T) {
	repository := plannerFixture(t, "leader")
	service, _ := New(Config{Channel: "stable", KernelVersion: "0.1.0", AllowedChannels: []string{"stable"}, AllowedPublishers: []string{"vastplan"}, AllowedPluginPrefixes: []string{"cn.vastplan"}})
	report, err := service.Plan(context.Background(), repository, planningRequest())
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != backendcompositionv1.ResolutionInvalid || len(report.Diagnostics) != 1 || !strings.Contains(report.Diagnostics[0].Message, "运行策略") {
		t.Fatalf("不兼容共置策略必须生成可解释 Invalid 报告: %+v", report)
	}
}

func TestFeatureDependenciesRemainScopedToSelectingService(t *testing.T) {
	repository := plannerFixture(t, "active-active")
	service, _ := New(Config{Channel: "stable", KernelVersion: "0.1.0", AllowedChannels: []string{"stable"}, AllowedPublishers: []string{"vastplan"}, AllowedPluginPrefixes: []string{"cn.vastplan"}})
	request := planningRequest()
	request.Intent.Services = append(request.Intent.Services, backendcompositionv1.ServiceIntent{
		ID: "worker", ServiceClass: "application.backend",
		RootPlugins:  []pluginv1.ArtifactRequirement{{PluginID: "cn.vastplan.product.agent.api", Constraint: "=1.0.0", Channel: "stable"}},
		PluginConfig: map[string]map[string]any{"cn.vastplan.product.agent.api": {"endpoint": "https://worker.example"}},
		Operations:   backendcompositionv1.ServiceOperationsIntent{Replicas: 1},
	})
	request.Intent, _ = backendcompositionv1.ValidateApplicationIntent(request.Intent)
	report, err := service.Plan(context.Background(), repository, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, unit := range report.ApplicationComposition.Units {
		if unit.Spec.ID == "worker" && len(unit.Spec.Plugins) != 1 {
			t.Fatalf("未选择 Feature 的 service 不得继承其他 service 的条件依赖: %+v", unit.Spec.Plugins)
		}
	}
}

func TestTrustedConfigurationSnapshotCompletesManagedCredentialWithoutExposingMaterial(t *testing.T) {
	repository := plannerFixture(t, "active-active")
	service, _ := New(Config{Channel: "stable", KernelVersion: "0.1.0", AllowedChannels: []string{"stable"}, AllowedPublishers: []string{"vastplan"}, AllowedPluginPrefixes: []string{"cn.vastplan"}})
	request := planningRequest()
	snapshot := backendcompositionv1.PlanningConfigurationSnapshot{Version: 1, Bindings: []backendcompositionv1.PlanningCredentialBinding{{
		UnitID: "api", PluginID: "cn.vastplan.product.agent.api", FieldID: "token",
		Ref: commonv1.ManagedCredentialRef{Handle: "credential://managed/agent-api", Scope: "tenant", Owner: "cn.vastplan.product.agent.api", Purpose: "agent.api", Version: 1},
	}}}
	snapshot.Digest = snapshot.ComputedDigest()
	request.ConfigurationSnapshot = &snapshot
	report, err := service.Plan(context.Background(), repository, request)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != backendcompositionv1.ResolutionResolved || len(report.ConfigurationPlan.Items[0].Missing) != 0 {
		t.Fatalf("可信配置快照应使方案收敛为 Resolved: %+v", report.ConfigurationPlan)
	}
	managed := report.ApplicationComposition.Units[0].Spec.Config["managed_credentials"].(map[string]any)
	if managed["cn.vastplan.product.agent.api"] == nil {
		t.Fatalf("已编译组合缺少不透明 CredentialRef: %+v", report.ApplicationComposition.Units[0].Spec.Config)
	}
	grants := report.ApplicationComposition.Units[0].Spec.Config["kernel_service_grants"].(map[string]any)
	if services, ok := grants["cn.vastplan.product.agent.api"].([]any); !ok || len(services) != 1 || services[0] != "kernel.config.credential-ref" {
		t.Fatalf("已编译组合缺少 Manifest 派生的精确 Kernel Service Grant: %+v", grants)
	}
	raw, _ := json.Marshal(report)
	if strings.Contains(string(raw), "secret-material") {
		t.Fatal("Resolution Report 不得包含凭证 material")
	}
}

func planningRequest() backendcompositionv1.PlanningRequest {
	intent := backendcompositionv1.ApplicationIntent{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "agent-suite"},
		Target:   compositioncommonv1.Target{Kernel: compositioncommonv1.KernelBackend}, Metadata: deploymentv1.Metadata{Name: "agent-suite", Tenant: "acme"},
		Services: []backendcompositionv1.ServiceIntent{{
			ID: "api", ServiceClass: "application.backend",
			RootPlugins:  []pluginv1.ArtifactRequirement{{PluginID: "cn.vastplan.product.agent.api", Constraint: "=1.0.0", Channel: "stable", Features: []string{"audit"}}},
			PluginConfig: map[string]map[string]any{"cn.vastplan.product.agent.api": {"endpoint": "https://api.example"}},
			Operations:   backendcompositionv1.ServiceOperationsIntent{Replicas: 2},
		}},
	}
	profile := backendcompositionv1.PlatformProfile{
		Document: compositioncommonv1.Document{Version: 1, Revision: 2, ID: "backend-default"}, Target: compositioncommonv1.Target{Kernel: compositioncommonv1.KernelBackend},
		ServiceClasses: []string{"application.backend"}, ServiceBaselines: []backendcompositionv1.ServiceBaseline{},
		Services: []deploymentv2.ServiceUnit{{
			ID: "settings", Kind: "service", Enabled: true, ServiceRole: "backend", LogicalService: "platform.settings",
			InstancePolicy: "leader", StateModel: "leader-owned", Visibility: "cluster", Routing: "leader", RoutingDomain: "platform", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{{ID: "cn.vastplan.platform.configuration.settings-provider", Version: "1.0.0", Channel: "stable"}},
		}},
	}
	intent, _ = backendcompositionv1.ValidateApplicationIntent(intent)
	profile, _ = backendcompositionv1.ValidatePlatformProfile(profile)
	return backendcompositionv1.PlanningRequest{Intent: intent, PlatformProfile: profile}
}

func plannerFixture(t *testing.T, auditPolicy string) *planningRepository {
	t.Helper()
	root := plannerDescriptor(t, "cn.vastplan.product.agent.api", "1.0.0", `
    "composition":{"features":[{"id":"audit","title":"审计","dependencies":{"cn.vastplan.product.agent.audit":"^1.0.0"},"configurationSchema":{"type":"object","additionalProperties":false,"properties":{"endpoint":{"type":"string","format":"uri"}},"required":["endpoint"]}}]},
    "configuration":{"scope":"service","applyMode":"restart","schema":{"type":"object","additionalProperties":false,"properties":{"endpoint":{"type":"string","format":"uri"}},"required":["endpoint"]},"managedCredentials":[{"id":"token","title":"Token","purpose":"agent.api","required":true}]},
    "runtime":{"instancePolicy":"active-active","stateModel":"external-shared","visibility":"cluster","routing":"queue","routingDomain":"application","provides":[{"extensionPoint":"tool.package","capability":"agent.api","contractVersion":"1.0.0","visibility":"cluster","routing":"queue","routingDomain":"application"}],"requires":[{"capability":"platform.settings","contractRange":"^1.0.0","scope":"remote","kind":"strong","ready":"readiness","failurePolicy":"fail","logicalService":"platform.settings","routingDomain":"platform"}]},
    "contributes":{"backend":{"tools":[{"id":"agent.api","service_role":"backend","title":"API","subcommands":[]}]}}`)
	auditRuntime := `"runtime":{"instancePolicy":"active-active","stateModel":"external-shared","visibility":"cluster","routing":"queue","routingDomain":"application","provides":[{"extensionPoint":"tool.package","capability":"agent.audit","contractVersion":"1.0.0","visibility":"cluster","routing":"queue","routingDomain":"application"}]}`
	if auditPolicy == "leader" {
		auditRuntime = `"runtime":{"instancePolicy":"leader","stateModel":"leader-owned","visibility":"cluster","routing":"leader","routingDomain":"application","provides":[{"extensionPoint":"tool.package","capability":"agent.audit","contractVersion":"1.0.0","visibility":"cluster","routing":"leader","routingDomain":"application"}]}`
	}
	audit := plannerDescriptor(t, "cn.vastplan.product.agent.audit", "1.2.0", auditRuntime+`,"contributes":{"backend":{"tools":[{"id":"agent.audit","service_role":"backend","title":"Audit","subcommands":[]}]}}`)
	settings := plannerDescriptor(t, "cn.vastplan.platform.configuration.settings-provider", "1.0.0", `
    "runtime":{"instancePolicy":"leader","stateModel":"leader-owned","visibility":"cluster","routing":"leader","routingDomain":"platform","provides":[{"extensionPoint":"tool.package","capability":"platform.settings","contractVersion":"1.0.0","visibility":"cluster","routing":"leader","routingDomain":"platform"}]},
    "contributes":{"backend":{"tools":[{"id":"platform.settings","service_role":"backend","title":"Settings","subcommands":[]}]}}`)
	return &planningRepository{descriptors: map[string]pluginv1.ArtifactPlanningDescriptor{
		root.Ref.PluginID: root, audit.Ref.PluginID: audit, settings.Ref.PluginID: settings,
	}}
}

func plannerDescriptor(t *testing.T, id, version, body string) pluginv1.ArtifactPlanningDescriptor {
	t.Helper()
	raw := []byte(fmt.Sprintf(`{"id":%q,"name":"Planner fixture","description":"Planner fixture","version":%q,"publisher":"vastplan","engines":{"backend":"^0.1"},"capabilities":{"kernelServices":["kernel.config.credential-ref"]},%s,"activation":["onStartup"],"entry":{"backend":"backend/main"}}`, id, version, body))
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatalf("fixture Manifest 无效: %v\n%s", err, raw)
	}
	canonical, _ := json.Marshal(manifest)
	return pluginv1.ArtifactPlanningDescriptor{
		Ref: pluginv1.ArtifactRef{PluginID: id, Version: version, Channel: "stable"}, SHA256: strings.Repeat(string('a'+rune(len(id)%6)), 64),
		Publisher: "vastplan", Manifest: canonical,
	}
}
