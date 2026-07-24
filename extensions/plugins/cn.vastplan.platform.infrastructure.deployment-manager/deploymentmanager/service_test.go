package deploymentmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
)

func TestDescriptorMatchesSignedManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var signed, runtime any
	if len(contributions) != 1 || json.Unmarshal(contributions[0].Descriptor, &signed) != nil || json.Unmarshal(Descriptor(), &runtime) != nil || !reflect.DeepEqual(signed, runtime) {
		t.Fatalf("运行时 descriptor 与签名 Manifest 不一致\nsigned=%s\nruntime=%s", contributions[0].Descriptor, Descriptor())
	}
}

type fakeHost struct {
	targets             []*contractv1.CallTarget
	err                 error
	readinessStatus     nodebootstrap.ReadinessStatus
	catalogEntry        *artifactCatalogEntry
	deploymentReadiness map[uint64]deploymentpublication.ReadinessObservation
	referenceSnapshots  []pluginv1.ArtifactReferenceSnapshot
	referenceErr        error
	referenceCalls      int
	failReferenceAt     int
	catalogCalls        int
}

func (h *fakeHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	h.targets = append(h.targets, target)
	if h.err != nil {
		return nil, nil, h.err
	}
	if target.Capability == nodebootstrap.KernelReadinessService {
		status := h.readinessStatus
		if status == "" {
			status = nodebootstrap.ReadinessWaiting
		}
		raw, _ := json.Marshal(nodebootstrap.ReadinessObservation{Status: status})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
	if target.Capability == deploymentpublication.KernelTargetsService {
		raw, _ := json.Marshal(map[string]any{"items": []deploymentpublication.Target{{DeploymentName: "agent-services", PlatformProfile: compositioncommonv1.Ref{ID: "backend-default", Revision: 1, Digest: strings.Repeat("a", 64)}}}})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
	if target.Capability == deploymentpublication.KernelPreviewService {
		var request deploymentpublication.PreviewRequest
		_ = json.Unmarshal(payload, &request)
		deployment := deploymentv2.Deployment{Version: 2, Revision: request.DeploymentRevision, Metadata: request.Composition.Metadata, Units: []deploymentv2.ServiceUnit{}}
		raw, _ := json.Marshal(deploymentpublication.Result{Deployment: deployment, Digest: fmt.Sprintf("preview-%d", request.DeploymentRevision), ArtifactReferences: fakeArtifactReferences(request.Composition)})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
	if target.Capability == deploymentpublication.KernelPublishService {
		var request deploymentpublication.PublishRequest
		_ = json.Unmarshal(payload, &request)
		deployment := deploymentv2.Deployment{Version: 2, Revision: request.DeploymentRevision, Metadata: request.Composition.Metadata, Units: []deploymentv2.ServiceUnit{}}
		raw, _ := json.Marshal(deploymentpublication.Result{Deployment: deployment, Digest: request.ExpectedDigest, KVRevision: request.DeploymentRevision + 10, ArtifactReferences: fakeArtifactReferences(request.Composition)})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
	if target.Capability == deploymentpublication.KernelReadinessService {
		var request deploymentpublication.ReadinessRequest
		_ = json.Unmarshal(payload, &request)
		observation, ok := h.deploymentReadiness[request.DeploymentRevision]
		if !ok {
			observation = deploymentpublication.ReadinessObservation{
				SchemaVersion: 1, Tenant: "tenant-a", Deployment: request.DeploymentName,
				Revision: request.DeploymentRevision, Status: deploymentpublication.ReadinessReady,
				UpdatedAt: time.Now().UTC(),
			}
		}
		raw, _ := json.Marshal(observation)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
	if target.Capability == platformadminapi.ArtifactsCapability && target.GetOperation() == "listCatalog" {
		h.catalogCalls++
		var query struct {
			Receipt artifactrepositoryv1.Receipt `json:"receipt"`
		}
		_ = json.Unmarshal(payload, &query)
		entry := artifactCatalogEntry{Ref: query.Receipt.Ref, SHA256: query.Receipt.SHA256, Publisher: "vastplan", RepositoryRevision: query.Receipt.Revision, Targets: []string{"backend"}}
		if h.catalogEntry != nil {
			entry = *h.catalogEntry
		}
		raw, _ := json.Marshal(entry)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
	if target.Capability == platformadminapi.ArtifactsCapability && target.GetOperation() == "putReferences" {
		h.referenceCalls++
		if h.failReferenceAt == h.referenceCalls {
			return nil, nil, errors.New("repository temporarily unavailable")
		}
		if h.referenceErr != nil {
			return nil, nil, h.referenceErr
		}
		var snapshot pluginv1.ArtifactReferenceSnapshot
		_ = json.Unmarshal(payload, &snapshot)
		h.referenceSnapshots = append(h.referenceSnapshots, snapshot)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"revision":1}`), nil
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"systemdActive":true}`), nil
}

func fakeArtifactReferences(composition backendcompositionv1.ApplicationComposition) []pluginv1.ArtifactReference {
	values := make([]pluginv1.ArtifactReference, 0)
	for _, unit := range composition.Units {
		for _, plugin := range unit.Spec.Plugins {
			channel := plugin.Channel
			if channel == "" {
				channel = "stable"
			}
			values = append(values, pluginv1.ArtifactReference{Ref: pluginv1.ArtifactRef{PluginID: plugin.ID, Version: plugin.Version, Channel: channel}, SHA256: strings.Repeat("c", 64), Purpose: "resolved"})
		}
	}
	return values
}

func testRepositoryReceipt(ref pluginv1.ArtifactRef, sha256 string, revision uint64) artifactrepositoryv1.Receipt {
	return artifactrepositoryv1.Receipt{
		SchemaVersion: 1, RepositoryID: "local-testing", Protocol: artifactrepositoryv1.ProtocolLocalTest,
		ProfileDigest: strings.Repeat("d", 64), Ref: ref, SHA256: sha256, Revision: revision,
	}
}

func TestBackendTestReleaseReadyAndAutomaticRollback(t *testing.T) {
	for _, test := range []struct {
		name       string
		candidate  deploymentpublication.ReadinessStatus
		wantStatus platformadminapi.TestReleaseStatus
		wantActive uint64
		wantTotal  int
	}{
		{name: "candidate ready", candidate: deploymentpublication.ReadinessReady, wantStatus: platformadminapi.TestReleaseReady, wantActive: 2, wantTotal: 2},
		{name: "candidate failed rolls back", candidate: deploymentpublication.ReadinessFailed, wantStatus: platformadminapi.TestReleaseRolledBack, wantActive: 3, wantTotal: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, err := openTestService(filepath.Join(t.TempDir(), "deployment-manager.json"))
			if err != nil {
				t.Fatal(err)
			}
			service.releaseTimeout, service.releasePollInterval = time.Second, time.Millisecond
			alice, bob, carol := userCall("tenant-a", "alice"), userCall("tenant-a", "bob"), userCall("tenant-a", "carol")
			composition := backendcompositionv1.ApplicationComposition{
				Metadata: deploymentv1.Metadata{Name: "agent-services"},
				Units: []backendcompositionv1.ApplicationUnit{{ServiceClass: "application", Spec: deploymentv2.ServiceUnit{
					ID: "api", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1,
					Plugins: []deploymentv1.PluginRef{{ID: "cn.example.application.demo", Version: "1.0.0", Channel: "stable"}},
				}}},
			}
			host := &fakeHost{}
			draft, err := service.CreateServiceDraft(context.Background(), host, alice, composition)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = service.SubmitServiceDraft(context.Background(), host, alice, draft.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = service.ApproveServiceRevision(bob, draft.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = service.PublishServiceRevision(context.Background(), host, carol, draft.ID); err != nil {
				t.Fatal(err)
			}
			if len(host.referenceSnapshots) < 3 || host.referenceSnapshots[0].Generation != 1 || host.referenceSnapshots[1].OwnerKind != "rollback-history" || host.referenceSnapshots[2].OwnerKind != "deployment-active" || host.referenceSnapshots[2].Generation != 2 {
				t.Fatalf("deployment reference protection must bracket activation and contract only after rollback protection: %+v", host.referenceSnapshots)
			}
			if host.catalogCalls != 0 || len(host.referenceSnapshots[2].References) != 1 {
				t.Fatalf("部署引用必须复用可信内核预览返回的多来源制品事实，不得旁路查询单一仓库: catalogCalls=%d snapshot=%+v", host.catalogCalls, host.referenceSnapshots[2])
			}
			binding, err := service.PutTestTargetBinding(carol, "demo-api", platformadminapi.PutTestTargetBindingRequest{
				Kind: platformadminapi.TestTargetBackend, Deployment: "agent-services", UnitID: "api",
				PluginID: "cn.example.application.demo", AllowedPublishers: []string{"vastplan"}, Enabled: true,
			})
			if err != nil || binding.Version != 1 {
				t.Fatalf("创建测试目标绑定失败: binding=%+v err=%v", binding, err)
			}
			artifact := pluginv1.ArtifactRef{PluginID: binding.PluginID, Version: "1.1.0-dev.1", Channel: "testing"}
			digest := strings.Repeat("a", 64)
			host.catalogEntry = &artifactCatalogEntry{
				Ref: artifact, SHA256: digest, Publisher: "vastplan", RepositoryRevision: 17, Targets: []string{"backend"},
			}
			host.deploymentReadiness = map[uint64]deploymentpublication.ReadinessObservation{
				2: {SchemaVersion: 1, Tenant: "tenant-a", Deployment: "agent-services", Revision: 2, Status: test.candidate, UpdatedAt: time.Now().UTC()},
				3: {SchemaVersion: 1, Tenant: "tenant-a", Deployment: "agent-services", Revision: 3, Status: deploymentpublication.ReadinessReady, UpdatedAt: time.Now().UTC()},
			}
			release, err := service.CreateTestRelease(context.Background(), host, carol, platformadminapi.CreateTestReleaseRequest{
				BindingID: binding.ID, Receipt: testRepositoryReceipt(artifact, digest, 17),
			})
			if err != nil || release.Status != test.wantStatus || release.RollbackRequired {
				t.Fatalf("测试发布终态错误: release=%+v err=%v", release, err)
			}
			var protected pluginv1.ArtifactReferenceSnapshot
			var released pluginv1.ArtifactReferenceSnapshot
			for _, snapshot := range host.referenceSnapshots {
				if snapshot.OwnerKind == "artifact-lock" && snapshot.Generation == 1 {
					protected = snapshot
				} else if snapshot.OwnerKind == "artifact-lock" && snapshot.Generation == 2 {
					released = snapshot
				}
			}
			if protected.OwnerKind != "artifact-lock" || len(protected.References) != 1 || protected.References[0].SHA256 != digest {
				t.Fatalf("测试制品必须在激活前取得精确引用保护: %+v", host.referenceSnapshots)
			}
			if released.Generation != 2 || len(released.References) != 0 {
				t.Fatalf("测试发布终态必须释放临时 artifact-lock: %+v", host.referenceSnapshots)
			}
			revisions, err := service.ListServiceRevisions(carol)
			if err != nil || len(revisions) != test.wantTotal || revisions[0].ID != test.wantActive || !revisions[0].Active {
				t.Fatalf("服务修订激活结果错误: %+v err=%v", revisions, err)
			}
			if test.candidate == deploymentpublication.ReadinessReady {
				plugin := revisions[0].Composition.Units[0].Spec.Plugins[0]
				if plugin.Version != artifact.Version || plugin.Channel != artifact.Channel {
					t.Fatalf("候选组合未锁定测试制品: %+v", plugin)
				}
			} else if release.RollbackServiceRevisionID != 3 || release.ErrorCode != "platform.test_release.candidate_not_ready" {
				t.Fatalf("自动回滚审计字段不完整: %+v", release)
			}
		})
	}
}

func TestTestReleaseReferenceProtectionFailsClosed(t *testing.T) {
	host := &fakeHost{referenceErr: errors.New("repository unavailable")}
	release := platformadminapi.TestRelease{ID: 7, Receipt: testRepositoryReceipt(pluginv1.ArtifactRef{PluginID: "cn.example.demo", Version: "1.0.0-dev.1", Channel: "testing"}, strings.Repeat("a", 64), 1)}
	if err := publishTestReleaseReference(context.Background(), host, userCall("tenant-a", "publisher"), release); err == nil {
		t.Fatal("candidate activation must not proceed without reference protection")
	}
}

func TestTestTargetBindingRejectsPlatformPlugin(t *testing.T) {
	service, err := openTestService(filepath.Join(t.TempDir(), "deployment-manager.json"))
	if err != nil {
		t.Fatal(err)
	}
	call := userCall("tenant-a", "alice")
	service.data.Tenants["tenant-a"] = &tenantState{
		Nodes: map[string]platformadminapi.ManagedNode{}, Jobs: map[string]platformadminapi.BootstrapJob{},
		TestBindings: map[string]platformadminapi.TestTargetBinding{},
		Revisions: []platformadminapi.ServiceRevision{{
			ID: 1, Deployment: "platform", Status: platformadminapi.ServicePublished, Active: true,
			Composition: backendcompositionv1.ApplicationComposition{Units: []backendcompositionv1.ApplicationUnit{{Spec: deploymentv2.ServiceUnit{
				ID: "core", Plugins: []deploymentv1.PluginRef{{ID: "cn.vastplan.platform.settings", Version: "1.0.0"}},
			}}}},
		}},
	}
	_, err = service.PutTestTargetBinding(call, "reserved", platformadminapi.PutTestTargetBindingRequest{
		Kind: platformadminapi.TestTargetBackend, Deployment: "platform", UnitID: "core",
		PluginID: "cn.vastplan.platform.settings", AllowedPublishers: []string{"vastplan"}, Enabled: true,
	})
	if !errors.Is(err, errInvalid) {
		t.Fatalf("foundation/platform 插件不得进入应用测试绑定: %v", err)
	}
}

func TestInterruptedTestReleaseCanBeRecoveredWithMonotonicRollback(t *testing.T) {
	service, err := openTestService(filepath.Join(t.TempDir(), "deployment-manager.json"))
	if err != nil {
		t.Fatal(err)
	}
	call := userCall("tenant-a", "operator")
	composition := backendcompositionv1.ApplicationComposition{
		Metadata: deploymentv1.Metadata{Name: "agent-services", Tenant: "tenant-a"},
		Units: []backendcompositionv1.ApplicationUnit{{ServiceClass: "application", Spec: deploymentv2.ServiceUnit{
			ID: "api", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{{ID: "cn.example.demo", Version: "1.0.0", Channel: "stable"}},
		}}},
	}
	service.data.Tenants["tenant-a"] = &tenantState{
		Nodes: map[string]platformadminapi.ManagedNode{}, Jobs: map[string]platformadminapi.BootstrapJob{},
		TestBindings: map[string]platformadminapi.TestTargetBinding{
			"demo": {ID: "demo", Kind: platformadminapi.TestTargetBackend, Deployment: "agent-services", UnitID: "api", PluginID: "cn.example.demo", AllowedPublishers: []string{"vastplan"}, Enabled: true, Version: 1},
		},
		NextRevision: 2, NextTestRelease: 1,
		Revisions: []platformadminapi.ServiceRevision{
			{ID: 1, Deployment: "agent-services", Status: platformadminapi.ServicePublished, Active: false, Composition: composition, PreviewDigest: "preview-1"},
			{ID: 2, Deployment: "agent-services", Status: platformadminapi.ServicePublished, Active: true, Composition: composition, PreviewDigest: "preview-2"},
		},
		TestReleases: []platformadminapi.TestRelease{{
			ID: 1, BindingID: "demo", Status: platformadminapi.TestReleaseFailed, RollbackRequired: true,
			PreviousServiceRevisionID: 1, CandidateServiceRevisionID: 2, RequestedBy: "operator",
		}},
	}
	host := &fakeHost{deploymentReadiness: map[uint64]deploymentpublication.ReadinessObservation{
		3: {SchemaVersion: 1, Tenant: "tenant-a", Deployment: "agent-services", Revision: 3, Status: deploymentpublication.ReadinessReady, UpdatedAt: time.Now().UTC()},
	}}
	recovered, err := service.RollbackTestRelease(context.Background(), host, call, 1)
	if err != nil || recovered.Status != platformadminapi.TestReleaseRolledBack || recovered.RollbackRequired || recovered.RollbackServiceRevisionID != 3 {
		t.Fatalf("中断发布恢复失败: release=%+v err=%v", recovered, err)
	}
	revisions, _ := service.ListServiceRevisions(call)
	if len(revisions) != 3 || revisions[0].ID != 3 || !revisions[0].Active {
		t.Fatalf("恢复必须生成新的单调 revision: %+v", revisions)
	}
}

func TestServiceCompositionWorkflowAndRollback(t *testing.T) {
	service, err := openTestService(filepath.Join(t.TempDir(), "deployment-manager.json"))
	if err != nil {
		t.Fatal(err)
	}
	host := &fakeHost{}
	alice, bob, carol := userCall("tenant-a", "alice"), userCall("tenant-a", "bob"), userCall("tenant-a", "carol")
	input := backendcompositionv1.ApplicationComposition{Metadata: deploymentv1.Metadata{Name: "agent-services"}, Units: []backendcompositionv1.ApplicationUnit{}}
	targets, err := service.ListDeploymentTargets(context.Background(), host, alice)
	if err != nil || len(targets) != 1 || targets[0].DeploymentName != "agent-services" {
		t.Fatalf("部署目标查询失败: %+v %v", targets, err)
	}
	draft, err := service.CreateServiceDraft(context.Background(), host, alice, input)
	if err != nil {
		t.Fatal(err)
	}
	if draft.ID != 1 || draft.Composition.Revision != 1 || draft.Composition.Metadata.Tenant != "tenant-a" || draft.PreviewDigest != "preview-1" {
		t.Fatalf("服务草稿未由服务端规范化: %+v", draft)
	}
	pending, err := service.SubmitServiceDraft(context.Background(), host, alice, draft.ID)
	if err != nil || pending.Status != platformadminapi.ServicePendingApproval || pending.SubmittedBy != "alice" {
		t.Fatalf("服务草稿提交失败: %+v %v", pending, err)
	}
	if _, err := service.ApproveServiceRevision(alice, draft.ID); !errors.Is(err, errSeparation) {
		t.Fatalf("提交人不得自批: %v", err)
	}
	approved, err := service.ApproveServiceRevision(bob, draft.ID)
	if err != nil || approved.Status != platformadminapi.ServiceApproved {
		t.Fatalf("服务修订审批失败: %+v %v", approved, err)
	}
	first, err := service.PublishServiceRevision(context.Background(), host, carol, draft.ID)
	if err != nil || !first.Active || first.KVRevision != 11 {
		t.Fatalf("服务修订发布失败: %+v %v", first, err)
	}
	secondDraft, err := service.CreateServiceDraft(context.Background(), host, alice, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SubmitServiceDraft(context.Background(), host, alice, secondDraft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApproveServiceRevision(bob, secondDraft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PublishServiceRevision(context.Background(), host, carol, secondDraft.ID); err != nil {
		t.Fatal(err)
	}
	rolledBack, err := service.RollbackServiceRevision(context.Background(), host, carol, first.ID)
	if err != nil || rolledBack.ID != 3 || !rolledBack.Active || rolledBack.Composition.Revision != 3 {
		t.Fatalf("回滚必须创建并发布新的单调修订: %+v %v", rolledBack, err)
	}
	revisions, err := service.ListServiceRevisions(alice)
	if err != nil || len(revisions) != 3 || !revisions[0].Active || revisions[1].Active || revisions[2].Active {
		t.Fatalf("服务组合激活状态错误: %+v %v", revisions, err)
	}
	audit, err := service.ListServiceRevisionAudit(alice, rolledBack.ID)
	if err != nil || len(audit) < 3 {
		t.Fatalf("回滚审计不完整: %+v %v", audit, err)
	}
}

func TestServiceReferenceOutboxRetriesAfterRepositoryRecovery(t *testing.T) {
	service, err := openTestService(filepath.Join(t.TempDir(), "deployment-manager.json"))
	if err != nil {
		t.Fatal(err)
	}
	host := &fakeHost{failReferenceAt: 2}
	alice, bob, carol := userCall("tenant-a", "alice"), userCall("tenant-a", "bob"), userCall("tenant-a", "carol")
	input := backendcompositionv1.ApplicationComposition{
		Metadata: deploymentv1.Metadata{Name: "agent-services"},
		Units: []backendcompositionv1.ApplicationUnit{{ServiceClass: "application", Spec: deploymentv2.ServiceUnit{
			ID: "api", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{{ID: "cn.example.application.demo", Version: "1.0.0", Channel: "stable"}},
		}}},
	}
	draft, err := service.CreateServiceDraft(context.Background(), host, alice, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.SubmitServiceDraft(context.Background(), host, alice, draft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ApproveServiceRevision(bob, draft.ID); err != nil {
		t.Fatal(err)
	}
	published, err := service.PublishServiceRevision(context.Background(), host, carol, draft.ID)
	if err != nil || !published.Active || !published.ReferencePending {
		t.Fatalf("仓库瞬时失败不得回滚已完成的内核切换，且必须留下 outbox: %+v err=%v", published, err)
	}
	if err = service.ReconcileServiceReferences(context.Background(), host, carol); err != nil {
		t.Fatalf("仓库恢复后必须能幂等收敛引用: %v", err)
	}
	revisions, err := service.ListServiceRevisions(carol)
	if err != nil || len(revisions) != 1 || revisions[0].ReferencePending {
		t.Fatalf("引用 outbox 未被清空: %+v err=%v", revisions, err)
	}
	if len(host.referenceSnapshots) != 3 || host.referenceSnapshots[2].OwnerKind != "deployment-active" || host.referenceSnapshots[2].Generation != 2 {
		t.Fatalf("重试后应先恢复回滚保护再收敛活动引用: %+v", host.referenceSnapshots)
	}
}

func (h *fakeHost) called(capability string) bool {
	for _, target := range h.targets {
		if target.Capability == capability {
			return true
		}
	}
	return false
}

func TestApprovalRequiresSeparationAndUsesKernelService(t *testing.T) {
	service, err := openTestService(filepath.Join(t.TempDir(), "deployment-manager.json"))
	if err != nil {
		t.Fatal(err)
	}
	service.newID = func() (string, error) { return "bootstrap-1", nil }
	alice := userCall("tenant-a", "alice")
	bob := userCall("tenant-a", "bob")
	node, err := service.PutNode(alice, "node-a", platformadminapi.PutManagedNodeRequest{Plan: validPlan()})
	if err != nil || node.Version != 1 {
		t.Fatalf("保存节点失败: %+v %v", node, err)
	}
	job, err := service.CreateJob(alice, "node-a")
	if err != nil || job.State != platformadminapi.BootstrapPending {
		t.Fatalf("创建引导作业失败: %+v %v", job, err)
	}
	if _, _, err := service.beginApproval(alice, job.ID); !errors.Is(err, errSeparation) {
		t.Fatalf("同一用户必须不能审批自己的请求: %v", err)
	}
	host := &fakeHost{}
	payload, _ := json.Marshal(map[string]string{"jobId": job.ID})
	result, raw, err := service.Handler(context.Background(), host, bob, payload, "approveBootstrap")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("审批执行失败: result=%+v err=%v", result, err)
	}
	var completed platformadminapi.BootstrapJob
	if err := json.Unmarshal(raw, &completed); err != nil || completed.State != platformadminapi.BootstrapSystemdActive || completed.ApprovedBy != "bob" {
		t.Fatalf("引导完成状态无效: %s err=%v", raw, err)
	}
	if !host.called(nodebootstrap.KernelService) || !host.called(nodebootstrap.KernelReadinessService) {
		t.Fatalf("插件没有调用固定引导与就绪内核服务: %+v", host.targets)
	}
}

func TestSignedLeaseObservationPromotesSystemdActiveToReady(t *testing.T) {
	service, err := openTestService(filepath.Join(t.TempDir(), "deployment-manager.json"))
	if err != nil {
		t.Fatal(err)
	}
	service.newID = func() (string, error) { return "bootstrap-ready", nil }
	alice := userCall("tenant-a", "alice")
	if _, err := service.PutNode(alice, "node-a", platformadminapi.PutManagedNodeRequest{Plan: validPlan()}); err != nil {
		t.Fatal(err)
	}
	job, err := service.CreateJob(alice, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]string{"jobId": job.ID})
	result, raw, err := service.Handler(context.Background(), &fakeHost{readinessStatus: nodebootstrap.ReadinessReady}, userCall("tenant-a", "bob"), payload, "approveBootstrap")
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("审批并观察就绪失败: %+v %v", result, err)
	}
	var ready platformadminapi.BootstrapJob
	if err := json.Unmarshal(raw, &ready); err != nil || ready.State != platformadminapi.BootstrapReady {
		t.Fatalf("签名 lease 应推进 Ready: %s %v", raw, err)
	}
}

func TestRejectedOrTimedOutLeaseFailsBootstrapJob(t *testing.T) {
	for _, test := range []struct {
		name      string
		status    nodebootstrap.ReadinessStatus
		advance   time.Duration
		errorCode string
	}{{name: "identity rejected", status: nodebootstrap.ReadinessRejected, errorCode: "platform.deployment.readiness_rejected"}, {name: "lease timeout", status: nodebootstrap.ReadinessWaiting, advance: jobTTL + time.Second, errorCode: "platform.deployment.readiness_timeout"}} {
		t.Run(test.name, func(t *testing.T) {
			service, err := openTestService(filepath.Join(t.TempDir(), "deployment-manager.json"))
			if err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
			service.now = func() time.Time { return now }
			service.newID = func() (string, error) { return "bootstrap-terminal", nil }
			alice := userCall("tenant-a", "alice")
			if _, err := service.PutNode(alice, "node-a", platformadminapi.PutManagedNodeRequest{Plan: validPlan()}); err != nil {
				t.Fatal(err)
			}
			job, err := service.CreateJob(alice, "node-a")
			if err != nil {
				t.Fatal(err)
			}
			payload, _ := json.Marshal(map[string]string{"jobId": job.ID})
			if result, _, err := service.Handler(context.Background(), &fakeHost{readinessStatus: test.status}, userCall("tenant-a", "bob"), payload, "approveBootstrap"); err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
				t.Fatalf("审批失败: %+v %v", result, err)
			}
			now = now.Add(test.advance)
			result, raw, err := service.Handler(context.Background(), &fakeHost{readinessStatus: test.status}, alice, []byte(`{}`), "listBootstrapJobs")
			if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
				t.Fatalf("查询作业失败: %+v %v", result, err)
			}
			var response struct {
				Items []platformadminapi.BootstrapJob `json:"items"`
			}
			if err := json.Unmarshal(raw, &response); err != nil || len(response.Items) != 1 || response.Items[0].State != platformadminapi.BootstrapFailed || response.Items[0].ErrorCode != test.errorCode {
				t.Fatalf("就绪失败状态不正确: %s %v", raw, err)
			}
		})
	}
}

func TestNodeCASAndActiveJobFreezeDefinition(t *testing.T) {
	service, err := openTestService(filepath.Join(t.TempDir(), "deployment-manager.json"))
	if err != nil {
		t.Fatal(err)
	}
	call := userCall("tenant-a", "alice")
	node, err := service.PutNode(call, "node-a", platformadminapi.PutManagedNodeRequest{Plan: validPlan()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutNode(call, "node-a", platformadminapi.PutManagedNodeRequest{Plan: validPlan()}); !errors.Is(err, errVersionConflict) {
		t.Fatalf("更新节点必须携带 CAS: %v", err)
	}
	if _, err := service.CreateJob(call, "node-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutNode(call, "node-a", platformadminapi.PutManagedNodeRequest{Plan: validPlan(), IfVersion: &node.Version}); !errors.Is(err, errJobConflict) {
		t.Fatalf("活动作业期间节点定义必须冻结: %v", err)
	}
}

func TestRestartFailsInterruptedBootstrapWithoutReexecution(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "deployment-manager.json")
	service, err := openTestService(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	service.newID = func() (string, error) { return "bootstrap-interrupted", nil }
	alice := userCall("tenant-a", "alice")
	if _, err := service.PutNode(alice, "node-a", platformadminapi.PutManagedNodeRequest{Plan: validPlan()}); err != nil {
		t.Fatal(err)
	}
	job, err := service.CreateJob(alice, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.beginApproval(userCall("tenant-a", "bob"), job.ID); err != nil {
		t.Fatal(err)
	}

	restarted, err := openTestService(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := restarted.ListJobs(alice)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("读取恢复作业失败: %+v %v", jobs, err)
	}
	if jobs[0].State != platformadminapi.BootstrapFailed || jobs[0].ErrorCode != "platform.deployment.interrupted" {
		t.Fatalf("重启必须把不确定的执行中作业转为人工可审计失败: %+v", jobs[0])
	}
}

func TestKernelBootstrapFailurePersistsStableFailure(t *testing.T) {
	service, err := openTestService(filepath.Join(t.TempDir(), "deployment-manager.json"))
	if err != nil {
		t.Fatal(err)
	}
	service.newID = func() (string, error) { return "bootstrap-failed", nil }
	alice := userCall("tenant-a", "alice")
	if _, err := service.PutNode(alice, "node-a", platformadminapi.PutManagedNodeRequest{Plan: validPlan()}); err != nil {
		t.Fatal(err)
	}
	job, err := service.CreateJob(alice, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]string{"jobId": job.ID})
	result, _, err := service.Handler(context.Background(), &fakeHost{err: errors.New("ssh failed")}, userCall("tenant-a", "bob"), payload, "approveBootstrap")
	if err != nil || result.GetError().GetCode() != "platform.deployment.bootstrap_failed" {
		t.Fatalf("内核失败必须映射稳定错误码: result=%+v err=%v", result, err)
	}
	jobs, err := service.ListJobs(alice)
	if err != nil || jobs[0].State != platformadminapi.BootstrapFailed || jobs[0].ErrorCode != "platform.deployment.bootstrap_failed" {
		t.Fatalf("失败作业必须持久化: %+v %v", jobs, err)
	}
}

func userCall(tenant, user string) *contractv1.CallContext {
	return &contractv1.CallContext{TenantId: tenant, Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: user}, Principal: &contractv1.Principal{TenantId: tenant, UserId: user}}
}

func validPlan() nodebootstrap.Plan {
	node := nodebootstrap.NodeAgent{
		ID: "node-a", Tenant: "tenant-a", Deployment: "production", Labels: "region=cn",
		NATSURL: "tls://nats.internal:4222", NATSCA: nodebootstrap.SecretsRoot + "/nats-ca.pem", NATSCert: nodebootstrap.SecretsRoot + "/node.crt", NATSKey: nodebootstrap.SecretsRoot + "/node.key", NATSSeed: nodebootstrap.SecretsRoot + "/node.seed",
		TransportSeed: nodebootstrap.SecretsRoot + "/transport.seed", TransportTrust: nodebootstrap.SecretsRoot + "/transport-trust.json",
		TransportPublicKey: testTransportPublicKey,
		RepositoryURL:      "https://artifacts.internal", RepositoryTrust: nodebootstrap.SecretsRoot + "/artifact-trust.json",
	}
	destinations := []string{node.NATSCA, node.NATSCert, node.NATSKey, node.NATSSeed, node.TransportSeed, node.TransportTrust, node.RepositoryTrust, nodebootstrap.ArtifactTokenFile}
	files := make([]nodebootstrap.CredentialSecretFile, 0, len(destinations))
	for i, destination := range destinations {
		files = append(files, nodebootstrap.CredentialSecretFile{Credential: "node-a.material-" + string(rune('a'+i)), Destination: destination, Mode: 0o440})
	}
	return nodebootstrap.Plan{
		Target:  nodebootstrap.Target{Address: "node-a.internal", User: "bootstrap"},
		Release: nodebootstrap.Release{Version: "1.0.0", URL: "https://releases.internal/backend-kernel", SHA256: strings.Repeat("a", 64)},
		Node:    node, SSHIdentityCredential: "node-a.ssh-identity", SSHKnownHostsCredential: "node-a.known-hosts", SecretFiles: files,
	}
}

const testTransportPublicKey = "UBN2AENL65VCM6XLPUDC4FGKH4EMJN2DKU2TVBDF34PRQTEG32FHOZ5G"
