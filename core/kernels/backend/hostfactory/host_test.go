package hostfactory

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
)

type bootstrapBroker struct{ called bool }

type readinessObserver struct{ called bool }

type deploymentController struct{ tenant string }

func (c *deploymentController) Targets(_ context.Context, tenant string) ([]deploymentpublication.Target, error) {
	c.tenant = tenant
	return []deploymentpublication.Target{{DeploymentName: "services"}}, nil
}
func (*deploymentController) Preview(_ context.Context, _ string, _ backendcompositionv1.ApplicationComposition, revision uint64) (deploymentpublication.Result, error) {
	return deploymentpublication.Result{Deployment: deploymentv2.Deployment{Revision: revision}}, nil
}
func (*deploymentController) Publish(_ context.Context, _ string, _ backendcompositionv1.ApplicationComposition, revision uint64, digest string) (deploymentpublication.Result, error) {
	return deploymentpublication.Result{Deployment: deploymentv2.Deployment{Revision: revision}, Digest: digest}, nil
}

func (o *readinessObserver) Observe(_ context.Context, expectation nodebootstrap.ReadinessExpectation) (nodebootstrap.ReadinessObservation, error) {
	o.called = expectation.TenantID == "tenant-a" && expectation.NodeID == "node-a"
	return nodebootstrap.ReadinessObservation{Status: nodebootstrap.ReadinessReady}, nil
}

func (b *bootstrapBroker) Bootstrap(_ context.Context, scope nodebootstrap.Scope, plan nodebootstrap.Plan) (nodebootstrap.Result, error) {
	b.called = scope.TenantID == plan.Node.Tenant
	return nodebootstrap.Result{SystemdActive: true, NodeID: plan.Node.ID}, nil
}

func TestNew_DefinesClosedBackendCatalogAndInternalService(t *testing.T) {
	host, err := New("1.0.0", t.Logf)
	if err != nil {
		t.Fatalf("创建 Backend 宿主失败: %v", err)
	}

	got := make([]string, 0)
	for _, point := range host.Registry.Points() {
		got = append(got, point.Name)
	}
	want := append(extpoint.BackendPluginPoints(), extpoint.KernelService)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Backend Registry 与封板目录漂移: got=%v want=%v", got, want)
	}

	service, ok := host.Registry.Lookup(extpoint.KernelService, "kernel.info")
	if !ok || service.PluginID != "__kernel__" {
		t.Fatalf("kernel.info 必须仅由内核登记: %+v ok=%v", service, ok)
	}
	diagnostics, ok := host.Registry.Lookup(extpoint.KernelService, "kernel.diagnostics")
	if !ok || diagnostics.PluginID != "__kernel__" {
		t.Fatalf("kernel.diagnostics 必须仅由内核登记: %+v ok=%v", diagnostics, ok)
	}
}

func TestKernelConfigGetRequiresAuthenticatedPluginAndReturnsFrozenValue(t *testing.T) {
	provider, err := kernelspi.NewMapConfig(map[string]any{"retries": 3})
	if err != nil {
		t.Fatal(err)
	}
	service := kernelConfigGet(provider)
	pluginCtx := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "plugin.a"}}
	result, payload, err := service(context.Background(), pluginCtx, []byte(`{"key":"retries"}`))
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("读取配置失败: %v %+v", err, result)
	}
	var retries int
	if err := json.Unmarshal(payload, &retries); err != nil || retries != 3 {
		t.Fatalf("配置值错误: %s", payload)
	}
	if _, _, err := service(context.Background(), &contractv1.CallContext{}, []byte(`{"key":"retries"}`)); err == nil {
		t.Fatal("非插件调用必须 fail-closed")
	}
}

func TestKernelNodeBootstrapAcceptsOnlyDeploymentManager(t *testing.T) {
	broker := &bootstrapBroker{}
	service := kernelNodeBootstrap(broker)
	plan := hostBootstrapPlan()
	payload, _ := json.Marshal(plan)
	trusted := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: nodebootstrap.DeploymentManagerPluginID}}
	result, _, err := service(context.Background(), trusted, payload)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || !broker.called {
		t.Fatalf("deployment-manager 可信调用失败: result=%+v err=%v", result, err)
	}
	untrusted := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "third.party"}}
	if _, _, err := service(context.Background(), untrusted, payload); err == nil {
		t.Fatal("其他插件不得调用节点引导内核服务")
	}
}

func TestKernelNodeReadinessAcceptsOnlyDeploymentManagerAndTenant(t *testing.T) {
	observer := &readinessObserver{}
	service := kernelNodeReadiness(observer)
	expectation := nodebootstrap.ReadinessExpectation{TenantID: "tenant-a", NodeID: "node-a", Deployment: "prod", TransportPublicKey: "UBN2AENL65VCM6XLPUDC4FGKH4EMJN2DKU2TVBDF34PRQTEG32FHOZ5G"}
	payload, _ := json.Marshal(expectation)
	trusted := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: nodebootstrap.DeploymentManagerPluginID}}
	result, _, err := service(context.Background(), trusted, payload)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || !observer.called {
		t.Fatalf("deployment-manager 就绪观察失败: result=%+v err=%v", result, err)
	}
	wrongTenant := expectation
	wrongTenant.TenantID = "tenant-b"
	payload, _ = json.Marshal(wrongTenant)
	if _, _, err := service(context.Background(), trusted, payload); err == nil {
		t.Fatal("跨租户就绪观察必须 fail-closed")
	}
}

func TestKernelDeploymentPublicationAcceptsOnlyDeploymentManager(t *testing.T) {
	controller := &deploymentController{}
	service := kernelDeploymentTargets(controller)
	trusted := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: deploymentpublication.DeploymentManagerPluginID}}
	result, raw, err := service(context.Background(), trusted, []byte(`{}`))
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || controller.tenant != "tenant-a" || !strings.Contains(string(raw), "services") {
		t.Fatalf("deployment-manager 目标查询失败: result=%+v raw=%s err=%v", result, raw, err)
	}
	untrusted := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "third.party"}}
	if _, _, err := service(context.Background(), untrusted, []byte(`{}`)); err == nil {
		t.Fatal("其他插件不得调用在线部署内核服务")
	}
	if _, _, err := service(context.Background(), trusted, []byte(`{"routingDomain":"attacker"}`)); err == nil {
		t.Fatal("内核部署服务必须拒绝未知路由字段")
	}
}

func hostBootstrapPlan() nodebootstrap.Plan {
	node := nodebootstrap.NodeAgent{ID: "node-a", Tenant: "tenant-a", Deployment: "prod", NATSURL: "tls://nats.internal:4222", NATSCA: nodebootstrap.SecretsRoot + "/nats-ca.pem", NATSCert: nodebootstrap.SecretsRoot + "/node.crt", NATSKey: nodebootstrap.SecretsRoot + "/node.key", NATSSeed: nodebootstrap.SecretsRoot + "/node.seed", TransportSeed: nodebootstrap.SecretsRoot + "/transport.seed", TransportTrust: nodebootstrap.SecretsRoot + "/transport-trust.json", TransportPublicKey: "UBN2AENL65VCM6XLPUDC4FGKH4EMJN2DKU2TVBDF34PRQTEG32FHOZ5G", RepositoryURL: "https://artifacts.internal", RepositoryTrust: nodebootstrap.SecretsRoot + "/artifact-trust.json"}
	plan := nodebootstrap.Plan{Target: nodebootstrap.Target{Address: "node-a.internal", User: "bootstrap"}, Release: nodebootstrap.Release{Version: "1.0.0", URL: "https://releases.internal/backend", SHA256: strings.Repeat("a", 64)}, Node: node, SSHIdentityCredential: "ssh.identity", SSHKnownHostsCredential: "ssh.known-hosts"}
	for i, destination := range []string{node.NATSCA, node.NATSCert, node.NATSKey, node.NATSSeed, node.TransportSeed, node.TransportTrust, node.RepositoryTrust, nodebootstrap.ArtifactTokenFile} {
		plan.SecretFiles = append(plan.SecretFiles, nodebootstrap.CredentialSecretFile{Credential: "material-" + string(rune('a'+i)), Destination: destination, Mode: 0o440})
	}
	return plan
}
