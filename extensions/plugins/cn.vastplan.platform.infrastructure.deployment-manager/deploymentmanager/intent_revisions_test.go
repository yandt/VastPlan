package deploymentmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
)

type intentWorkflowHost struct {
	profile           backendcompositionv1.PlatformProfile
	plannerGeneration byte
	plannerStatus     string
	approvalDecision  *approvalv2.Decision
}

func (h *intentWorkflowHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	switch target.Capability {
	case deploymentpublication.KernelTargetsService:
		ref := compositioncommonv1.Ref{ID: h.profile.ID, Revision: h.profile.Revision, Digest: h.profile.Digest()}
		return okJSON(map[string]any{"items": []deploymentpublication.Target{{DeploymentName: "agent-services", PlatformProfile: ref, PlanningProfile: h.profile}}})
	case backendcompositionv1.PlanningCapability:
		var request backendcompositionv1.PlanningRequest
		if err := json.Unmarshal(payload, &request); err != nil {
			return nil, nil, err
		}
		report, err := fakeResolutionReport(request, h.plannerGeneration, h.plannerStatus)
		if err != nil {
			return nil, nil, err
		}
		return okJSON(report)
	case deploymentpublication.KernelPreviewService:
		var request deploymentpublication.PreviewRequest
		_ = json.Unmarshal(payload, &request)
		return okJSON(h.publicationResult(request.Composition, request.DeploymentRevision, "preview"))
	case deploymentpublication.KernelPublishService:
		var request deploymentpublication.PublishRequest
		_ = json.Unmarshal(payload, &request)
		result := h.publicationResult(request.Composition, request.DeploymentRevision, "publish")
		result.Digest, result.KVRevision = request.ExpectedDigest, 17
		return okJSON(result)
	case deploymentpublication.KernelReadinessService:
		var request deploymentpublication.ReadinessRequest
		_ = json.Unmarshal(payload, &request)
		return okJSON(deploymentpublication.ReadinessObservation{SchemaVersion: 1, Tenant: "tenant-a", Deployment: request.DeploymentName, Revision: request.DeploymentRevision, Generation: request.DeploymentRevision, Status: deploymentpublication.ReadinessReady, UpdatedAt: time.Now().UTC()})
	case platformadminapi.ArtifactsCapability:
		return okJSON(map[string]any{"revision": 1})
	case approvalv2.Capability:
		if h.approvalDecision == nil {
			return nil, nil, fmt.Errorf("approval decision unavailable")
		}
		return okJSON(approvalv2.EvaluateResult{Decision: *h.approvalDecision})
	default:
		return nil, nil, fmt.Errorf("unexpected capability %s", target.Capability)
	}
}

func (h *intentWorkflowHost) publicationResult(composition backendcompositionv1.ApplicationComposition, revision uint64, prefix string) deploymentpublication.Result {
	profile := compositioncommonv1.Ref{ID: h.profile.ID, Revision: h.profile.Revision, Digest: h.profile.Digest()}
	application := compositioncommonv1.Ref{ID: composition.ID, Revision: composition.Revision, Digest: composition.Digest()}
	deployment := deploymentv2.Deployment{
		Version: 2, Revision: revision, Metadata: composition.Metadata,
		Resolution: deploymentv2.Resolution{PlatformProfile: profile, ApplicationComposition: application, PluginOrigins: map[string]string{"cn.vastplan.product.agent.api": deploymentv2.OriginApplication}},
		Units:      []deploymentv2.ServiceUnit{composition.Units[0].Spec},
	}
	return deploymentpublication.Result{
		Deployment: deployment, Digest: strings.Repeat(prefix[:1], 64), PlatformProfile: profile,
		ArtifactReferences: []pluginv1.ArtifactReference{{Ref: pluginv1.ArtifactRef{PluginID: "cn.vastplan.product.agent.api", Version: "1.0.0", Channel: "stable"}, SHA256: strings.Repeat("a", 64), Purpose: "resolved"}},
	}
}

func TestIntentWorkflowBindsPlanAndRevokesApprovalWhenStale(t *testing.T) {
	service, err := openTestService(t.TempDir() + "/deployment-manager.json")
	if err != nil {
		t.Fatal(err)
	}
	host := &intentWorkflowHost{profile: intentPlatformProfile(), plannerGeneration: '1'}
	alice, bob, carol := userCall("tenant-a", "alice"), userCall("tenant-a", "bob"), userCall("tenant-a", "carol")
	draft, err := service.CreateIntentDraft(context.Background(), host, alice, intentFixture())
	if err != nil || draft.Intent == nil || draft.ResolutionReport == nil || draft.ResolutionReport.Status != backendcompositionv1.ResolutionResolved {
		t.Fatalf("Intent 草稿没有形成可发布计划: %+v err=%v", draft, err)
	}
	originalPlanDigest := draft.ResolutionReport.PlanDigest
	pending, err := service.SubmitServiceDraft(context.Background(), host, alice, draft.ID)
	if err != nil || pending.SubmittedPlanDigest != pending.ResolutionReport.PlanDigest {
		t.Fatalf("提交没有绑定 planDigest: %+v err=%v", pending, err)
	}
	host.plannerGeneration = '2'
	if _, err := service.ApproveServiceRevision(context.Background(), host, bob, draft.ID); err != errPlanStale {
		t.Fatalf("Planner 输入漂移必须阻止审批并标记 stale: %v", err)
	}
	stale, _ := service.ListServiceRevisions(alice)
	if !stale[0].PlanningStale || stale[0].Status != platformadminapi.ServiceDraft || stale[0].SubmittedPlanDigest != "" {
		t.Fatalf("stale 必须撤销已提交摘要和审批状态: %+v", stale[0])
	}
	audit, _ := service.ListServiceRevisionAudit(alice, draft.ID)
	if audit[len(audit)-1].Action != "service.intent.stale" || audit[len(audit)-1].PlanDigest != originalPlanDigest {
		t.Fatalf("stale 审计必须永久保留被撤销的计划摘要: %+v", audit)
	}
	host.plannerGeneration = '1'
	if _, err := service.SubmitServiceDraft(context.Background(), host, alice, draft.ID); err != errPlanStale {
		t.Fatalf("即使外部输入恢复为旧摘要，也必须显式刷新 stale 草稿: %v", err)
	}
	host.plannerGeneration = '2'
	refreshed, err := service.RefreshIntentPlan(context.Background(), host, alice, draft.ID)
	if err != nil || refreshed.PlanningStale || refreshed.ResolutionReport.Planner.ConfigurationDigest != strings.Repeat("2", 64) {
		t.Fatalf("显式刷新未接受新计划: %+v err=%v", refreshed, err)
	}
	if _, err = service.SubmitServiceDraft(context.Background(), host, alice, draft.ID); err != nil {
		t.Fatal(err)
	}
	approved, err := service.ApproveServiceRevision(context.Background(), host, bob, draft.ID)
	if err != nil || approved.ApprovedPlanDigest != approved.ResolutionReport.PlanDigest {
		t.Fatalf("审批没有绑定计划摘要: %+v err=%v", approved, err)
	}
	published, err := service.PublishServiceRevision(context.Background(), host, carol, draft.ID)
	if err != nil || !published.Active || published.Status != platformadminapi.ServicePublished {
		t.Fatalf("Intent 计划未通过既有内核入口发布: %+v err=%v", published, err)
	}
}

func TestInvalidIntentPlanIsPersistedButCannotEnterApproval(t *testing.T) {
	service, err := openTestService(t.TempDir() + "/deployment-manager.json")
	if err != nil {
		t.Fatal(err)
	}
	host := &intentWorkflowHost{profile: intentPlatformProfile(), plannerGeneration: '1', plannerStatus: backendcompositionv1.ResolutionInvalid}
	alice := userCall("tenant-a", "alice")
	draft, err := service.CreateIntentDraft(context.Background(), host, alice, intentFixture())
	if err != nil || draft.ResolutionReport == nil || draft.ResolutionReport.Status != backendcompositionv1.ResolutionInvalid || draft.PreviewDigest != "" {
		t.Fatalf("Invalid 计划应作为可解释草稿持久化且不得生成内核预览: %+v err=%v", draft, err)
	}
	if _, err := service.SubmitServiceDraft(context.Background(), host, alice, draft.ID); err != errPlanNotReady {
		t.Fatalf("Invalid 草稿不得进入审批: %v", err)
	}
}

func fakeResolutionReport(request backendcompositionv1.PlanningRequest, generation byte, status string) (backendcompositionv1.ResolutionReport, error) {
	requirement := request.Intent.Services[0].RootPlugins[0]
	root := pluginv1.ArtifactRef{PluginID: requirement.PluginID, Version: strings.TrimLeft(requirement.Constraint, "=^"), Channel: requirement.Channel}
	planner := backendcompositionv1.PlannerIdentity{Ref: pluginv1.ArtifactRef{PluginID: "cn.vastplan.platform.infrastructure.composition-planner", Version: "0.1.0", Channel: "stable"}, Capability: backendcompositionv1.PlanningCapability, ConfigurationDigest: strings.Repeat(string(generation), 64)}
	intentRef := compositioncommonv1.Ref{ID: request.Intent.ID, Revision: request.Intent.Revision, Digest: request.Intent.Digest()}
	profileRef := compositioncommonv1.Ref{ID: request.PlatformProfile.ID, Revision: request.PlatformProfile.Revision, Digest: request.PlatformProfile.Digest()}
	if status == backendcompositionv1.ResolutionInvalid {
		return backendcompositionv1.FinalizeResolutionReport(backendcompositionv1.ResolutionReport{
			Version: 1, Intent: intentRef, PlatformProfile: profileRef, Planner: planner, Status: backendcompositionv1.ResolutionInvalid,
			Features: []backendcompositionv1.ResolvedFeature{}, ProviderBindings: []backendcompositionv1.CapabilityProviderBinding{},
			ServiceGraph:      backendcompositionv1.ServiceDependencyGraph{Nodes: []backendcompositionv1.ServiceDependencyNode{}, Edges: []backendcompositionv1.ServiceDependencyEdge{}},
			ConfigurationPlan: backendcompositionv1.ConfigurationPlan{Items: []backendcompositionv1.ConfigurationPlanItem{}},
			Diagnostics:       []backendcompositionv1.ResolutionDiagnostic{{Severity: "error", Code: "fixture.invalid", Message: "invalid fixture"}},
		})
	}
	unit := backendcompositionv1.ApplicationUnit{ServiceClass: request.Intent.Services[0].ServiceClass, Spec: deploymentv2.ServiceUnit{
		ID: request.Intent.Services[0].ID, Kind: "service", Enabled: true, ServiceRole: "backend", LogicalService: request.Intent.ID + ".api",
		InstancePolicy: "active-active", StateModel: "external-shared", Visibility: "cluster", Routing: "queue", RoutingDomain: "application", Replicas: 1,
		Plugins: []deploymentv1.PluginRef{{ID: root.PluginID, Version: root.Version, Channel: root.Channel}},
	}}
	composition := backendcompositionv1.ApplicationComposition{Document: request.Intent.Document, Target: request.Intent.Target, Metadata: request.Intent.Metadata, Units: []backendcompositionv1.ApplicationUnit{unit}}
	lock := pluginv1.ArtifactLock{
		SchemaVersion: "v1", RepositoryRevision: 5, Target: "backend", KernelVersion: "0.1.0",
		Roots:    []pluginv1.ArtifactRequirement{{PluginID: root.PluginID, Constraint: "=" + root.Version, Channel: root.Channel}},
		Packages: []pluginv1.ArtifactLockPackage{{Ref: root, SHA256: strings.Repeat("a", 64), Size: 1, Publisher: "vastplan", KeyID: "release", RepositoryRevision: 5}},
	}
	lock.Digest, _ = pluginv1.ArtifactLockDigest(lock)
	report := backendcompositionv1.ResolutionReport{
		Version:         1,
		Intent:          intentRef,
		PlatformProfile: profileRef,
		Planner:         planner,
		Status:          backendcompositionv1.ResolutionResolved, ApplicationComposition: &composition, ArtifactLock: &lock,
		Features: []backendcompositionv1.ResolvedFeature{}, ProviderBindings: []backendcompositionv1.CapabilityProviderBinding{},
		ServiceGraph:      backendcompositionv1.ServiceDependencyGraph{Nodes: []backendcompositionv1.ServiceDependencyNode{{UnitID: "api", ServiceClass: "application.backend"}}, Edges: []backendcompositionv1.ServiceDependencyEdge{}},
		ConfigurationPlan: backendcompositionv1.ConfigurationPlan{Items: []backendcompositionv1.ConfigurationPlanItem{}}, Diagnostics: []backendcompositionv1.ResolutionDiagnostic{},
	}
	return backendcompositionv1.FinalizeResolutionReport(report)
}

func intentFixture() backendcompositionv1.ApplicationIntent {
	return backendcompositionv1.ApplicationIntent{
		Metadata: deploymentv1.Metadata{Name: "agent-services"},
		Services: []backendcompositionv1.ServiceIntent{{ID: "api", ServiceClass: "application.backend", RootPlugins: []pluginv1.ArtifactRequirement{{PluginID: "cn.vastplan.product.agent.api", Constraint: "=1.0.0", Channel: "stable"}}, Operations: backendcompositionv1.ServiceOperationsIntent{Replicas: 1}}},
	}
}

func intentPlatformProfile() backendcompositionv1.PlatformProfile {
	profile := backendcompositionv1.PlatformProfile{Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "backend-default"}, Target: compositioncommonv1.Target{Kernel: compositioncommonv1.KernelBackend}, ServiceClasses: []string{"application.backend"}, ServiceBaselines: []backendcompositionv1.ServiceBaseline{}, Services: []deploymentv2.ServiceUnit{}}
	profile, _ = backendcompositionv1.ValidatePlatformProfile(profile)
	return profile
}

func okJSON(value any) (*contractv1.CallResult, []byte, error) {
	raw, err := json.Marshal(value)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
}
