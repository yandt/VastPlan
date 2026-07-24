package deploymentmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformprofileactivation"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

const configuredPlatformPlugin = "cn.vastplan.platform.example.configurable"

type profileActivationHost struct {
	manifest       []byte
	unit           deploymentv2.ServiceUnit
	prepared       platformprofileactivation.PrepareRequest
	preparedResult platformprofileactivation.PrepareResult
	status         platformprofileactivation.Status
	readiness      map[uint64]deploymentpublication.ReadinessStatus
	rollbackCalls  int
}

func (h *profileActivationHost) Call(_ context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	switch target.GetCapability() {
	case platformprofileactivation.KernelPrepareService:
		var request platformprofileactivation.PrepareRequest
		_ = json.Unmarshal(payload, &request)
		digest, _ := platformprofileactivation.DigestPrepareRequest(request)
		preview, err := h.result(call.GetTenantId(), request.Composition, request.DeploymentRevision, "west", 0)
		if err != nil {
			return nil, nil, err
		}
		h.prepared, h.status = request, platformprofileactivation.StatusPrepared
		h.preparedResult = platformprofileactivation.PrepareResult{
			Candidate: platformprofileactivation.Candidate{
				CandidateID: request.CandidateID, RequestDigest: digest, ConfigurationID: request.ConfigurationID, Deployment: request.Composition.Metadata.Name,
				PreviousProfile:       compositioncommonv1.Ref{ID: "platform-default", Revision: 1, Digest: strings.Repeat("1", 64)},
				NextProfile:           compositioncommonv1.Ref{ID: "platform-default", Revision: 2, Digest: strings.Repeat("2", 64)},
				ExpectedCatalogDigest: strings.Repeat("3", 64), NextCatalogDigest: strings.Repeat("4", 64), Status: h.status,
			},
			Preview: preview,
		}
		return profileHostResult(h.preparedResult)
	case platformprofileactivation.KernelActivateService:
		h.status = platformprofileactivation.StatusActivated
		candidate := h.preparedResult.Candidate
		candidate.Status = h.status
		return profileHostResult(candidate)
	case platformprofileactivation.KernelPublishService:
		if h.status != platformprofileactivation.StatusActivated {
			return nil, nil, fmt.Errorf("candidate not active")
		}
		result := h.preparedResult.Preview
		result.KVRevision = result.Deployment.Revision + 100
		return profileHostResult(result)
	case platformprofileactivation.KernelFinalizeService:
		h.status = platformprofileactivation.StatusFinalized
		candidate := h.preparedResult.Candidate
		candidate.Status = h.status
		return profileHostResult(candidate)
	case platformprofileactivation.KernelAbortService:
		h.status = platformprofileactivation.StatusAborted
		candidate := h.preparedResult.Candidate
		candidate.Status = h.status
		return profileHostResult(candidate)
	case platformprofileactivation.KernelRollbackService:
		h.rollbackCalls++
		h.status = platformprofileactivation.StatusRolledBack
		candidate := h.preparedResult.Candidate
		candidate.Status, candidate.RollbackCatalogDigest = h.status, strings.Repeat("5", 64)
		return profileHostResult(candidate)
	case deploymentpublication.KernelPreviewService:
		var request deploymentpublication.PreviewRequest
		_ = json.Unmarshal(payload, &request)
		result, err := h.result(call.GetTenantId(), request.Composition, request.DeploymentRevision, "east", 0)
		return activationHostResult(result, err)
	case deploymentpublication.KernelPublishService:
		var request deploymentpublication.PublishRequest
		_ = json.Unmarshal(payload, &request)
		result, err := h.result(call.GetTenantId(), request.Composition, request.DeploymentRevision, "east", request.DeploymentRevision+100)
		return activationHostResult(result, err)
	case deploymentpublication.KernelReadinessService:
		var request deploymentpublication.ReadinessRequest
		_ = json.Unmarshal(payload, &request)
		status := h.readiness[request.DeploymentRevision]
		if status == "" {
			status = deploymentpublication.ReadinessReady
		}
		return profileHostResult(deploymentpublication.ReadinessObservation{SchemaVersion: 1, Tenant: call.GetTenantId(), Deployment: request.DeploymentName, Revision: request.DeploymentRevision, Status: status, UpdatedAt: time.Now().UTC()})
	case platformadminapi.ArtifactsCapability:
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"revision":1}`), nil
	default:
		return nil, nil, fmt.Errorf("unexpected target: %+v", target)
	}
}

func (h *profileActivationHost) result(tenant string, composition backendcompositionv1.ApplicationComposition, revision uint64, region string, kvRevision uint64) (deploymentpublication.Result, error) {
	unit := cloneJSON(h.unit)
	unit.Config = map[string]any{"plugins": map[string]any{configuredPlatformPlugin: map[string]any{"region": region}}}
	deployment := deploymentv2.Deployment{
		Version: 2, Revision: revision, Metadata: deploymentv1.Metadata{Name: composition.Metadata.Name, Tenant: tenant}, Units: []deploymentv2.ServiceUnit{unit},
		Resolution: deploymentv2.Resolution{
			PlatformProfile:        compositioncommonv1.Ref{ID: "platform-default", Revision: 1, Digest: strings.Repeat("1", 64)},
			ApplicationComposition: compositioncommonv1.Ref{ID: composition.ID, Revision: composition.Revision, Digest: composition.Digest()},
			PluginOrigins:          map[string]string{configuredPlatformPlugin: deploymentv2.OriginPlatformProfile},
			PluginBaselines:        map[string]string{configuredPlatformPlugin: "application-platform-config"},
		},
	}
	ref := pluginv1.ArtifactRef{PluginID: configuredPlatformPlugin, Version: "1.0.0", Channel: "stable"}
	catalog, err := pluginconfiguration.Build(deployment, map[pluginv1.ArtifactRef]pluginv1.Artifact{ref: {PluginID: ref.PluginID, Version: ref.Version, Channel: ref.Channel, SHA256: strings.Repeat("a", 64), Manifest: h.manifest}})
	if err != nil {
		return deploymentpublication.Result{}, err
	}
	return deploymentpublication.Result{
		Deployment: deployment, Digest: deployment.Digest(), KVRevision: kvRevision,
		PlatformCatalogDigest: strings.Repeat("4", 64), PlatformProfile: compositioncommonv1.Ref{ID: "platform-default", Revision: 2, Digest: strings.Repeat("2", 64)},
		ArtifactReferences: []pluginv1.ArtifactReference{{Ref: ref, SHA256: strings.Repeat("a", 64), Purpose: "resolved"}}, ConfigurationCatalog: catalog,
	}, nil
}

func profileHostResult(value any) (*contractv1.CallResult, []byte, error) {
	raw, err := json.Marshal(value)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
}

func TestProfileConfigurationActivationRecoversAndReachesReady(t *testing.T) {
	service, request, host := profileActivationFixture(t)
	alice, bob := pluginSettingsUserCall("tenant-a", "alice"), pluginSettingsUserCall("tenant-a", "bob")
	activation, err := service.CreateProfileConfigurationActivation(context.Background(), host, alice, request)
	if err != nil || activation.Status != platformprofileactivation.ActivationPendingApproval || activation.DeploymentRevision != 2 {
		t.Fatalf("Profile 候选未进入独立审批: activation=%+v err=%v", activation, err)
	}
	if _, err := service.ApproveProfileConfigurationActivation(alice, platformprofileactivation.ActivationLookup{CandidateID: request.CandidateID}); err == nil {
		t.Fatal("Profile 配置提交人不得自批")
	}
	if _, err := service.ApproveProfileConfigurationActivation(bob, platformprofileactivation.ActivationLookup{CandidateID: request.CandidateID}); err != nil {
		t.Fatal(err)
	}
	restarted, err := openTestService(testStateFile(service))
	if err != nil {
		t.Fatalf("重启后无法恢复 Profile Activation: %v", err)
	}
	restarted.releaseTimeout, restarted.releasePollInterval = time.Second, time.Millisecond
	service = restarted
	ready, err := service.PublishProfileConfigurationActivation(context.Background(), host, bob, platformprofileactivation.ActivationLookup{CandidateID: request.CandidateID})
	if err != nil || ready.Status != platformprofileactivation.ActivationReady || host.status != platformprofileactivation.StatusFinalized {
		t.Fatalf("Profile Activation 未收敛 Ready: activation=%+v kernel=%s err=%v", ready, host.status, err)
	}
	revisions, _ := service.ListServiceRevisions(bob)
	if len(revisions) != 2 || revisions[0].ID != 2 || !revisions[0].Active || revisions[0].ConfigurationCandidateID != "" {
		t.Fatalf("Profile 激活应生成新的活动解析修订且不冒充 Application Activation: %+v", revisions)
	}
	definition := revisions[0].ConfigurationCatalog.Items[0]
	if !jsonEqual(definition.Values, []byte(`{"region":"west"}`)) {
		t.Fatalf("活动配置目录未切换到 Profile 候选: %s", definition.Values)
	}
	if _, err := service.CreateProfileConfigurationActivation(context.Background(), host, alice, request); err != nil {
		t.Fatalf("终态相同请求必须幂等: %v", err)
	}
}

func TestProfileConfigurationReadinessFailureRollsBackCatalogAndDeployment(t *testing.T) {
	service, request, host := profileActivationFixture(t)
	host.readiness[2] = deploymentpublication.ReadinessFailed
	host.readiness[3] = deploymentpublication.ReadinessReady
	alice, bob := pluginSettingsUserCall("tenant-a", "alice"), pluginSettingsUserCall("tenant-a", "bob")
	if _, err := service.CreateProfileConfigurationActivation(context.Background(), host, alice, request); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApproveProfileConfigurationActivation(bob, platformprofileactivation.ActivationLookup{CandidateID: request.CandidateID}); err != nil {
		t.Fatal(err)
	}
	rolledBack, err := service.PublishProfileConfigurationActivation(context.Background(), host, bob, platformprofileactivation.ActivationLookup{CandidateID: request.CandidateID})
	if err != nil || rolledBack.Status != platformprofileactivation.ActivationRolledBack || rolledBack.RollbackDeploymentRevision != 3 || host.rollbackCalls != 1 {
		t.Fatalf("Profile readiness 失败未完成双重单调回滚: activation=%+v rollbackCalls=%d err=%v", rolledBack, host.rollbackCalls, err)
	}
	revisions, _ := service.ListServiceRevisions(bob)
	if len(revisions) != 3 || revisions[0].ID != 3 || !revisions[0].Active || revisions[1].Active || revisions[2].Active {
		t.Fatalf("Profile 回滚后的活动解析修订错误: %+v", revisions)
	}
	if !jsonEqual(revisions[0].ConfigurationCatalog.Items[0].Values, []byte(`{"region":"east"}`)) {
		t.Fatal("回滚 Deployment 未恢复旧 Profile 配置")
	}
}

func TestProfileConfigurationPendingCandidateCanAbortAndUnlock(t *testing.T) {
	service, request, host := profileActivationFixture(t)
	alice := pluginSettingsUserCall("tenant-a", "alice")
	if _, err := service.CreateProfileConfigurationActivation(context.Background(), host, alice, request); err != nil {
		t.Fatal(err)
	}
	if !profileActivationLocksDeployment(service.data.Tenants["tenant-a"], "services-a") {
		t.Fatal("待审批 Profile 候选必须锁定目标 deployment")
	}
	aborted, err := service.AbortProfileConfigurationActivation(context.Background(), host, alice, platformprofileactivation.ActivationLookup{CandidateID: request.CandidateID})
	if err != nil || aborted.Status != platformprofileactivation.ActivationAborted || profileActivationLocksDeployment(service.data.Tenants["tenant-a"], "services-a") {
		t.Fatalf("放弃候选未释放目标锁: activation=%+v err=%v", aborted, err)
	}
}

func TestProfileConfigurationOperationsRejectDirectUserCaller(t *testing.T) {
	service, request, host := profileActivationFixture(t)
	if _, err := service.CreateProfileConfigurationActivation(context.Background(), host, userCall("tenant-a", "alice"), request); err == nil {
		t.Fatal("浏览器用户不得绕过 plugin-settings 直接创建 Profile Activation")
	}
}

func pluginSettingsUserCall(tenant, user string) *contractv1.CallContext {
	return &contractv1.CallContext{
		TenantId: tenant, Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: pluginconfiguration.PluginSettingsID},
		Principal: &contractv1.Principal{UserId: user},
	}
}

func profileActivationFixture(t *testing.T) (*Service, platformprofileactivation.CreateActivationRequest, *profileActivationHost) {
	t.Helper()
	manifest := []byte(fmt.Sprintf(`{"id":%q,"name":"Configured platform","description":"configured","version":"1.0.0","publisher":"vastplan","engines":{"backend":"^0.1"},"configuration":{"scope":"service","applyMode":"restart","schema":{"type":"object","additionalProperties":false,"required":["region"],"properties":{"region":{"type":"string"}}}},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}}`, configuredPlatformPlugin))
	unit := deploymentv2.ServiceUnit{ID: "platform-core", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1, Plugins: []deploymentv1.PluginRef{{ID: configuredPlatformPlugin, Version: "1.0.0", Channel: "stable"}}}
	host := &profileActivationHost{manifest: manifest, unit: unit, readiness: map[uint64]deploymentpublication.ReadinessStatus{}}
	composition := backendcompositionv1.ApplicationComposition{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "services-a"}, Target: compositioncommonv1.Target{Kernel: compositioncommonv1.KernelBackend},
		Metadata: deploymentv1.Metadata{Name: "services-a", Tenant: "tenant-a"}, Units: []backendcompositionv1.ApplicationUnit{},
	}
	initial, err := host.result("tenant-a", composition, 1, "east", 101)
	if err != nil {
		t.Fatal(err)
	}
	if len(initial.ConfigurationCatalog.Items) != 1 {
		t.Fatalf("公共基线配置夹具应生成 1 条目录定义，实际 %d", len(initial.ConfigurationCatalog.Items))
	}
	service, err := openTestService(filepath.Join(t.TempDir(), "deployment-manager.json"))
	if err != nil {
		t.Fatal(err)
	}
	service.releaseTimeout, service.releasePollInterval = time.Second, time.Millisecond
	now := time.Now().UTC().Format(time.RFC3339Nano)
	service.data.Tenants["tenant-a"] = &tenantState{
		NextRevision: 1, Nodes: map[string]platformadminapi.ManagedNode{}, Jobs: map[string]platformadminapi.BootstrapJob{},
		TestBindings: map[string]platformadminapi.TestTargetBinding{}, ProfileActivations: map[string]profileActivationRecord{},
		Revisions: []platformadminapi.ServiceRevision{{
			ID: 1, Deployment: "services-a", Status: platformadminapi.ServicePublished, Active: true,
			Composition: composition, Preview: initial.Deployment, PreviewDigest: initial.Digest,
			ArtifactReferences: initial.ArtifactReferences, ConfigurationCatalog: initial.ConfigurationCatalog, CreatedAt: now, UpdatedAt: now,
		}},
	}
	definition := initial.ConfigurationCatalog.Items[0]
	request := platformprofileactivation.CreateActivationRequest{
		CandidateID: "pcfg_" + strings.Repeat("b", 32), ConfigurationID: definition.ID, ConfigCatalogDigest: initial.ConfigurationCatalog.Digest,
		SchemaDigest: definition.SchemaDigest, ArtifactSHA256: definition.Artifact.SHA256, Values: json.RawMessage(`{"region":"west"}`),
	}
	return service, request, host
}
