package backendcompositionv1

import (
	"encoding/json"
	"strings"
	"testing"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestApplicationIntentIsNarrowAndDeterministic(t *testing.T) {
	left, err := ParseApplicationIntent([]byte(`{
  "version":1,"revision":3,"id":"agent-services","target":{"kernel":"backend"},"metadata":{"name":"agent-services","tenant":"acme"},
  "services":[
    {"id":"worker","serviceClass":"application.backend","rootPlugins":[{"ref":{"pluginId":"cn.vastplan.product.agent.worker","version":"1.2.0","channel":"stable"},"features":["trace","audit"]}],"operations":{"replicas":2}},
    {"id":"api","serviceClass":"application.backend","rootPlugins":[{"ref":{"pluginId":"cn.vastplan.product.agent.api","version":"1.0.0","channel":"stable"}}],"pluginConfig":{"cn.vastplan.product.agent.api":{"limit":10}},"operations":{"replicas":1}}
  ]
}`))
	if err != nil {
		t.Fatal(err)
	}
	right, err := ParseApplicationIntent([]byte(`{
  "version":1,"revision":3,"id":"agent-services","target":{"kernel":"backend"},"metadata":{"tenant":"acme","name":"agent-services"},
  "services":[
    {"id":"api","serviceClass":"application.backend","rootPlugins":[{"ref":{"channel":"stable","version":"1.0.0","pluginId":"cn.vastplan.product.agent.api"}}],"pluginConfig":{"cn.vastplan.product.agent.api":{"limit":10}},"operations":{"replicas":1}},
    {"id":"worker","serviceClass":"application.backend","rootPlugins":[{"ref":{"pluginId":"cn.vastplan.product.agent.worker","version":"1.2.0","channel":"stable"},"features":["audit","trace"]}],"operations":{"replicas":2}}
  ]
}`))
	if err != nil {
		t.Fatal(err)
	}
	if left.Digest() != right.Digest() {
		t.Fatalf("同一 Intent 的 service/feature 声明顺序不应改变摘要: %s != %s", left.Digest(), right.Digest())
	}
	if got, want := left.Digest(), "c5dfab4c35ae3b65e496eea0ddf658ddb40d8da4d1aa43c99c39d8fea9db42a9"; got != want {
		t.Fatalf("Application Intent 跨语言规范摘要漂移: got=%s want=%s", got, want)
	}
	invalid := `{
  "version":1,"revision":1,"id":"bad","target":{"kernel":"backend"},"metadata":{"name":"bad"},
  "services":[{"id":"api","serviceClass":"application.backend","rootPlugins":[{"ref":{"pluginId":"cn.vastplan.product.agent.api","version":"1.0.0","channel":"stable"}}],"operations":{"replicas":1},"depends_on":["database"]}]
}`
	if _, err := ParseApplicationIntent([]byte(invalid)); err == nil {
		t.Fatal("Application Intent 必须拒绝 depends_on 等内部执行字段")
	}
}

func TestResolutionReportBindsPlanAndRejectsInvalidGraph(t *testing.T) {
	intent, err := ParseApplicationIntent([]byte(`{
  "version":1,"revision":1,"id":"agent-services","target":{"kernel":"backend"},"metadata":{"name":"agent-services"},
  "services":[{"id":"api","serviceClass":"application.backend","rootPlugins":[{"ref":{"pluginId":"cn.vastplan.product.agent.api","version":"1.0.0","channel":"stable"}}],"operations":{"replicas":1}}]
}`))
	if err != nil {
		t.Fatal(err)
	}
	composition, err := ParseApplicationComposition([]byte(`{
  "version":1,"revision":1,"id":"agent-services","target":{"kernel":"backend"},"metadata":{"name":"agent-services"},
  "units":[{"serviceClass":"application.backend","spec":{"id":"api","kind":"service","plugins":[{"id":"cn.vastplan.product.agent.api","version":"1.0.0","channel":"stable"}],"enabled":true,"service_role":"backend","replicas":1}}]
}`))
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	report := ResolutionReport{
		Version:         1,
		Intent:          compositioncommonv1.Ref{ID: intent.ID, Revision: intent.Revision, Digest: intent.Digest()},
		PlatformProfile: compositioncommonv1.Ref{ID: "backend-default", Revision: 2, Digest: strings.Repeat("b", 64)},
		Planner: PlannerIdentity{Ref: pluginv1.ArtifactRef{
			PluginID: "cn.vastplan.platform.infrastructure.composition-planner", Version: "0.1.0", Channel: "stable",
		}, Capability: "platform.composition.plan"},
		Status:                 ResolutionResolved,
		ApplicationComposition: &composition,
		ArtifactLock: &pluginv1.ArtifactLock{
			SchemaVersion: "v1", RepositoryRevision: 9, Target: "backend", KernelVersion: "0.1.0",
			Roots: []pluginv1.ArtifactRequirement{{PluginID: "cn.vastplan.product.agent.api", Constraint: "=1.0.0"}},
			Packages: []pluginv1.ArtifactLockPackage{{
				Ref:    pluginv1.ArtifactRef{PluginID: "cn.vastplan.product.agent.api", Version: "1.0.0", Channel: "stable"},
				SHA256: digest, Size: 100, Publisher: "vastplan", KeyID: "local", RepositoryRevision: 9,
			}},
			Digest: strings.Repeat("c", 64),
		},
		Features:         []ResolvedFeature{},
		ProviderBindings: []CapabilityProviderBinding{},
		ServiceGraph: ServiceDependencyGraph{
			Nodes: []ServiceDependencyNode{{UnitID: "api", ServiceClass: "application.backend"}}, Edges: []ServiceDependencyEdge{},
		},
		ConfigurationPlan: ConfigurationPlan{Items: []ConfigurationPlanItem{{
			UnitID: "api", PluginID: "cn.vastplan.product.agent.api", Source: "root", Editable: true,
			SchemaDigest: strings.Repeat("d", 64), ConfigurationDigest: strings.Repeat("e", 64),
			DependencyPath: []string{"cn.vastplan.product.agent.api"},
		}}},
		Diagnostics: []ResolutionDiagnostic{},
	}
	lockDigest, err := pluginv1.ArtifactLockDigest(*report.ArtifactLock)
	if err != nil {
		t.Fatal(err)
	}
	report.ArtifactLock.Digest = lockDigest
	finalized, err := FinalizeResolutionReport(report)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(finalized)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseResolutionReport(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.PlanDigest != finalized.PlanDigest || len(parsed.PlanDigest) != 64 {
		t.Fatalf("Resolution Report 未绑定规范摘要: %+v", parsed)
	}

	cyclic := cloneResolutionReport(t, finalized)
	cyclic.ServiceGraph.Nodes = append(cyclic.ServiceGraph.Nodes, ServiceDependencyNode{UnitID: "worker", ServiceClass: "application.backend"})
	cyclic.ServiceGraph.Edges = []ServiceDependencyEdge{
		{FromUnitID: "api", ToUnitID: "worker", Capability: "worker", Kind: "strong", FailurePolicy: "fail"},
		{FromUnitID: "worker", ToUnitID: "api", Capability: "api", Kind: "strong", FailurePolicy: "fail"},
	}
	cyclic.PlanDigest = cyclic.ComputedPlanDigest()
	if _, err := ValidateResolutionReport(cyclic); err == nil {
		t.Fatal("Resolution Report service graph 必须拒绝依赖环")
	}

	unknownPlugin := cloneResolutionReport(t, finalized)
	unknownPlugin.ConfigurationPlan.Items[0].PluginID = "cn.vastplan.product.unknown"
	unknownPlugin.ConfigurationPlan.Items[0].DependencyPath = []string{"cn.vastplan.product.unknown"}
	unknownPlugin.ConfigurationPlan.Digest = unknownPlugin.ConfigurationPlan.ComputedDigest()
	unknownPlugin.PlanDigest = unknownPlugin.ComputedPlanDigest()
	if _, err := ValidateResolutionReport(unknownPlugin); err == nil {
		t.Fatal("Resolution Report 必须拒绝 unit 中不存在的配置插件")
	}

	extraNode := cloneResolutionReport(t, finalized)
	extraNode.ServiceGraph.Nodes = append(extraNode.ServiceGraph.Nodes, ServiceDependencyNode{UnitID: "extra", ServiceClass: "application.backend"})
	extraNode.PlanDigest = extraNode.ComputedPlanDigest()
	if _, err := ValidateResolutionReport(extraNode); err == nil {
		t.Fatal("Resolution Report service graph 不得包含 Composition 之外的 unit")
	}
}

func cloneResolutionReport(t *testing.T, source ResolutionReport) ResolutionReport {
	t.Helper()
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var target ResolutionReport
	if err := json.Unmarshal(raw, &target); err != nil {
		t.Fatal(err)
	}
	return target
}

func TestNeedsConfigurationReportRequiresMissingFields(t *testing.T) {
	report := ResolutionReport{
		Status: ResolutionNeedsConfiguration,
		ConfigurationPlan: ConfigurationPlan{Items: []ConfigurationPlanItem{{
			UnitID: "api", PluginID: "cn.vastplan.product.agent.api", Source: "root", Editable: true,
			DependencyPath: []string{"cn.vastplan.product.agent.api"},
			Missing:        []ConfigurationRequirement{{Kind: "property", Field: "endpoint"}},
		}}},
	}
	if err := validateResolutionStatus(report); err != nil {
		t.Fatalf("配置缺失应映射为 NeedsConfiguration: %v", err)
	}
	report.ConfigurationPlan.Items[0].Missing = nil
	if err := validateResolutionStatus(report); err == nil {
		t.Fatal("没有缺失项时不得伪装为 NeedsConfiguration")
	}
}
