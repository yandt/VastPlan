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
	targets         []*contractv1.CallTarget
	err             error
	readinessStatus nodebootstrap.ReadinessStatus
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
		raw, _ := json.Marshal(deploymentpublication.Result{Deployment: deployment, Digest: fmt.Sprintf("preview-%d", request.DeploymentRevision)})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
	if target.Capability == deploymentpublication.KernelPublishService {
		var request deploymentpublication.PublishRequest
		_ = json.Unmarshal(payload, &request)
		deployment := deploymentv2.Deployment{Version: 2, Revision: request.DeploymentRevision, Metadata: request.Composition.Metadata, Units: []deploymentv2.ServiceUnit{}}
		raw, _ := json.Marshal(deploymentpublication.Result{Deployment: deployment, Digest: request.ExpectedDigest, KVRevision: request.DeploymentRevision + 10})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"systemdActive":true}`), nil
}

func TestServiceCompositionWorkflowAndRollback(t *testing.T) {
	service, err := New(filepath.Join(t.TempDir(), "deployment-manager.json"))
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

func (h *fakeHost) called(capability string) bool {
	for _, target := range h.targets {
		if target.Capability == capability {
			return true
		}
	}
	return false
}

func TestApprovalRequiresSeparationAndUsesKernelService(t *testing.T) {
	service, err := New(filepath.Join(t.TempDir(), "deployment-manager.json"))
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
	service, err := New(filepath.Join(t.TempDir(), "deployment-manager.json"))
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
			service, err := New(filepath.Join(t.TempDir(), "deployment-manager.json"))
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
	service, err := New(filepath.Join(t.TempDir(), "deployment-manager.json"))
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
	service, err := New(stateFile)
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

	restarted, err := New(stateFile)
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
	service, err := New(filepath.Join(t.TempDir(), "deployment-manager.json"))
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
