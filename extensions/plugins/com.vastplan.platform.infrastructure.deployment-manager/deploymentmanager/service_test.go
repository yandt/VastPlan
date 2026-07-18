package deploymentmanager

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
)

type fakeHost struct {
	target *contractv1.CallTarget
	err    error
}

func (h *fakeHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
	h.target = target
	if h.err != nil {
		return nil, nil, h.err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"systemdActive":true}`), nil
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
	if host.target == nil || host.target.ExtensionPoint != "kernel.service" || host.target.Capability != nodebootstrap.KernelService {
		t.Fatalf("插件没有调用固定内核服务: %+v", host.target)
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
		RepositoryURL: "https://artifacts.internal", RepositoryTrust: nodebootstrap.SecretsRoot + "/artifact-trust.json",
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
