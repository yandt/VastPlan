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
	"cdsoft.com.cn/VastPlan/core/shared/go/configurationactivation"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

const configuredApplicationPlugin = "cn.example.configured-application"

type configurationActivationHost struct {
	manifest  []byte
	readiness map[uint64]deploymentpublication.ReadinessStatus
}

func (h *configurationActivationHost) Call(_ context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	switch target.GetCapability() {
	case deploymentpublication.KernelPreviewService:
		var request deploymentpublication.PreviewRequest
		_ = json.Unmarshal(payload, &request)
		result, err := h.result(call.GetTenantId(), request.Composition, request.DeploymentRevision, 0)
		return activationHostResult(result, err)
	case deploymentpublication.KernelPublishService:
		var request deploymentpublication.PublishRequest
		_ = json.Unmarshal(payload, &request)
		result, err := h.result(call.GetTenantId(), request.Composition, request.DeploymentRevision, request.DeploymentRevision+100)
		return activationHostResult(result, err)
	case deploymentpublication.KernelReadinessService:
		var request deploymentpublication.ReadinessRequest
		_ = json.Unmarshal(payload, &request)
		status := h.readiness[request.DeploymentRevision]
		if status == "" {
			status = deploymentpublication.ReadinessReady
		}
		raw, _ := json.Marshal(deploymentpublication.ReadinessObservation{SchemaVersion: 1, Tenant: call.GetTenantId(), Deployment: request.DeploymentName, Revision: request.DeploymentRevision, Status: status, UpdatedAt: time.Now().UTC()})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	case platformadminapi.ArtifactsCapability:
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"revision":1}`), nil
	default:
		return nil, nil, fmt.Errorf("unexpected target: %+v", target)
	}
}

func (h *configurationActivationHost) result(tenant string, composition backendcompositionv1.ApplicationComposition, revision, kvRevision uint64) (deploymentpublication.Result, error) {
	units := make([]deploymentv2.ServiceUnit, len(composition.Units))
	for i := range composition.Units {
		units[i] = composition.Units[i].Spec
	}
	deployment := deploymentv2.Deployment{Version: 2, Revision: revision, Metadata: composition.Metadata, Units: units, Resolution: deploymentv2.Resolution{PluginOrigins: map[string]string{configuredApplicationPlugin: deploymentv2.OriginApplication}}}
	ref := pluginv1.ArtifactRef{PluginID: configuredApplicationPlugin, Version: "1.0.0", Channel: "stable"}
	catalog, err := pluginconfiguration.Build(deployment, map[pluginv1.ArtifactRef]pluginv1.Artifact{ref: {PluginID: ref.PluginID, Version: ref.Version, Channel: ref.Channel, SHA256: strings.Repeat("a", 64), Manifest: h.manifest}})
	if err != nil {
		return deploymentpublication.Result{}, err
	}
	return deploymentpublication.Result{Deployment: deployment, Digest: deployment.Digest(), KVRevision: kvRevision, ConfigurationCatalog: catalog}, nil
}

func activationHostResult(result deploymentpublication.Result, err error) (*contractv1.CallResult, []byte, error) {
	if err != nil {
		return nil, nil, err
	}
	raw, _ := json.Marshal(result)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func TestApplicationConfigurationActivationIsGovernedAndReady(t *testing.T) {
	service, request, host := configuredActivationFixture(t)
	alice, bob := userCall("tenant-a", "alice"), userCall("tenant-a", "bob")
	activation, err := service.CreateConfigurationActivation(context.Background(), host, alice, request)
	if err != nil || activation.Status != configurationactivation.StatusPendingApproval || activation.ServiceRevision != 2 {
		t.Fatalf("配置修订未进入审批: activation=%+v err=%v", activation, err)
	}
	if _, err := service.ApproveServiceRevision(alice, activation.ServiceRevision); err == nil {
		t.Fatal("配置提交人不得自批")
	}
	if _, err := service.ApproveServiceRevision(bob, activation.ServiceRevision); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PublishServiceRevision(context.Background(), host, bob, activation.ServiceRevision); err == nil {
		t.Fatal("配置绑定修订不得绕过专用 readiness/rollback 流程发布")
	}
	changed := request
	changed.Values = json.RawMessage(`{"region":"cn-south"}`)
	if _, err := service.CreateConfigurationActivation(context.Background(), host, alice, changed); err == nil {
		t.Fatal("同一候选 ID 不得将不同请求误判为幂等重试")
	}
	restarted, err := openTestService(testStateFile(service))
	if err != nil {
		t.Fatalf("重启后无法恢复配置激活摘要: %v", err)
	}
	restarted.releaseTimeout, restarted.releasePollInterval = time.Second, time.Millisecond
	retry, err := restarted.CreateConfigurationActivation(context.Background(), host, alice, request)
	if err != nil || retry.ServiceRevision != activation.ServiceRevision {
		t.Fatalf("重启后相同请求未幂等恢复: activation=%+v err=%v", retry, err)
	}
	service = restarted
	ready, err := service.PublishConfigurationActivation(context.Background(), host, bob, configurationactivation.LookupRequest{CandidateID: request.CandidateID})
	if err != nil || ready.Status != configurationactivation.StatusReady {
		t.Fatalf("配置候选未收敛 Ready: activation=%+v err=%v", ready, err)
	}
	revisions, _ := service.ListServiceRevisions(bob)
	if len(revisions) != 2 || !revisions[0].Active || revisions[0].ConfigurationCandidateID != request.CandidateID {
		t.Fatalf("活动配置修订错误: %+v", revisions)
	}
	envelope, err := pluginconfig.Parse(revisions[0].Composition.Units[0].Spec.Config, []string{configuredApplicationPlugin})
	if err != nil || envelope.Plugins[configuredApplicationPlugin]["region"] != "cn-west" || envelope.ManagedCredentials[configuredApplicationPlugin]["token"].Handle != request.Credentials["token"].Handle {
		t.Fatalf("配置或凭证引用未进入新修订: envelope=%+v err=%v", envelope, err)
	}
}

func TestApplicationConfigurationReadinessFailureRollsBackMonotonically(t *testing.T) {
	service, request, host := configuredActivationFixture(t)
	host.readiness[2] = deploymentpublication.ReadinessFailed
	host.readiness[3] = deploymentpublication.ReadinessReady
	alice, bob := userCall("tenant-a", "alice"), userCall("tenant-a", "bob")
	activation, err := service.CreateConfigurationActivation(context.Background(), host, alice, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApproveServiceRevision(bob, activation.ServiceRevision); err != nil {
		t.Fatal(err)
	}
	rolledBack, err := service.PublishConfigurationActivation(context.Background(), host, bob, configurationactivation.LookupRequest{CandidateID: request.CandidateID})
	if err != nil || rolledBack.Status != configurationactivation.StatusRolledBack || rolledBack.RollbackServiceRevision != 3 {
		t.Fatalf("readiness 失败未单调回滚: activation=%+v err=%v", rolledBack, err)
	}
	revisions, _ := service.ListServiceRevisions(bob)
	if len(revisions) != 3 || revisions[0].ID != 3 || !revisions[0].Active || revisions[1].Active || revisions[2].Active {
		t.Fatalf("回滚活动修订错误: %+v", revisions)
	}
}

func TestPublicServiceRevisionRedactsManagedCredentialHandles(t *testing.T) {
	service, _, _ := configuredActivationFixture(t)
	revision := service.data.Tenants["tenant-a"].Revisions[0]
	public := publicServiceRevision(revision)
	if _, ok := public.Composition.Units[0].Spec.Config[pluginconfig.ManagedCredentialsKey]; ok {
		t.Fatal("浏览器可见 Composition 不得包含托管凭证句柄")
	}
	if _, ok := public.Preview.Units[0].Config[pluginconfig.ManagedCredentialsKey]; ok {
		t.Fatal("浏览器可见 Preview 不得包含托管凭证句柄")
	}
	if _, ok := revision.Composition.Units[0].Spec.Config[pluginconfig.ManagedCredentialsKey]; !ok {
		t.Fatal("公开裁剪不得修改内部不可变修订")
	}
}

func configuredActivationFixture(t *testing.T) (*Service, configurationactivation.CreateRequest, *configurationActivationHost) {
	t.Helper()
	manifest := []byte(fmt.Sprintf(`{"id":%q,"name":"Configured application","description":"configured","version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},"capabilities":{"kernelServices":["kernel.config.credential-ref"]},"configuration":{"scope":"service","applyMode":"restart","schema":{"type":"object","additionalProperties":false,"required":["region"],"properties":{"region":{"type":"string"}}},"managedCredentials":[{"id":"token","title":"Token","purpose":"remote.token","required":true}]},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}}`, configuredApplicationPlugin))
	host := &configurationActivationHost{manifest: manifest, readiness: map[uint64]deploymentpublication.ReadinessStatus{}}
	oldRef := pluginconfig.ManagedCredentialRef{Handle: "credential://managed/old", Scope: "tenant", Owner: configuredApplicationPlugin, Purpose: "remote.token", Version: 1}
	composition := backendcompositionv1.ApplicationComposition{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "agent-services"}, Target: compositioncommonv1.Target{Kernel: compositioncommonv1.KernelBackend}, Metadata: deploymentv1.Metadata{Name: "agent-services", Tenant: "tenant-a"},
		Units: []backendcompositionv1.ApplicationUnit{{ServiceClass: "application", Spec: deploymentv2.ServiceUnit{ID: "api", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1, Plugins: []deploymentv1.PluginRef{{ID: configuredApplicationPlugin, Version: "1.0.0", Channel: "stable"}}, Config: map[string]any{"plugins": map[string]any{configuredApplicationPlugin: map[string]any{"region": "cn-east"}}, "managed_credentials": map[string]any{configuredApplicationPlugin: map[string]any{"token": oldRef}}}}}},
	}
	initial, err := host.result("tenant-a", composition, 1, 101)
	if err != nil {
		t.Fatal(err)
	}
	service, err := openTestService(filepath.Join(t.TempDir(), "deployment-manager.json"))
	if err != nil {
		t.Fatal(err)
	}
	service.releaseTimeout, service.releasePollInterval = time.Second, time.Millisecond
	service.data.Tenants["tenant-a"] = &tenantState{NextRevision: 1, Nodes: map[string]platformadminapi.ManagedNode{}, Jobs: map[string]platformadminapi.BootstrapJob{}, TestBindings: map[string]platformadminapi.TestTargetBinding{}, Revisions: []platformadminapi.ServiceRevision{{ID: 1, Deployment: "agent-services", Status: platformadminapi.ServicePublished, Active: true, Composition: composition, Preview: initial.Deployment, PreviewDigest: initial.Digest, ConfigurationCatalog: initial.ConfigurationCatalog, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}}
	definition := initial.ConfigurationCatalog.Items[0]
	request := configurationactivation.CreateRequest{CandidateID: "pcfg_" + strings.Repeat("b", 32), ConfigurationID: definition.ID, CatalogDigest: initial.ConfigurationCatalog.Digest, SchemaDigest: definition.SchemaDigest, ArtifactSHA256: definition.Artifact.SHA256, Values: json.RawMessage(`{"region":"cn-west"}`), Credentials: map[string]pluginconfig.ManagedCredentialRef{"token": {Handle: "credential://managed/new", Scope: "tenant", Owner: configuredApplicationPlugin, Purpose: "remote.token", Version: 2}}}
	return service, request, host
}
