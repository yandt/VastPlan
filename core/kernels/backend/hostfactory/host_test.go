package hostfactory

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	"cdsoft.com.cn/VastPlan/core/shared/go/configurationauthority"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/credentiallease"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	"cdsoft.com.cn/VastPlan/core/shared/go/runtimeidentity"
)

type bootstrapBroker struct{ called bool }

type readinessObserver struct{ called bool }

type deploymentController struct{ tenant string }

type deploymentReadinessObserver struct{ called bool }

type configurationCatalogReader struct{ tenant string }

type configurationAuthorityPort struct {
	issuedTenant, consumedTenant, consumedToken string
}

func (p *configurationAuthorityPort) Issue(_ context.Context, tenant string, request configurationauthority.IssueRequest) (configurationauthority.Issued, error) {
	p.issuedTenant = tenant
	return configurationauthority.Issued{Token: configurationauthority.TokenPrefix + strings.Repeat("a", 64), ExpiresAt: time.Now().UTC().Add(time.Minute)}, nil
}

func (p *configurationAuthorityPort) Consume(_ context.Context, tenant, token string) (configurationauthority.Claims, error) {
	p.consumedTenant, p.consumedToken = tenant, token
	return configurationauthority.Claims{TenantID: tenant, CandidateID: "pcfg_" + strings.Repeat("b", 32)}, nil
}

func (r *configurationCatalogReader) List(_ context.Context, tenant string) ([]pluginconfiguration.Catalog, error) {
	r.tenant = tenant
	return []pluginconfiguration.Catalog{}, nil
}

func TestConfigurationAuthorityKernelServicesEnforceExactPluginIdentities(t *testing.T) {
	port := &configurationAuthorityPort{}
	issue := kernelConfigurationAuthorityIssue(port)
	issuePayload := []byte(`{"configurationId":"cfg_123","catalogDigest":"digest","candidateId":"pcfg_123","fieldId":"token"}`)
	coordinator := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: configurationauthority.CoordinatorPluginID}}
	if _, _, err := issue(context.Background(), coordinator, issuePayload); err != nil || port.issuedTenant != "tenant-a" {
		t.Fatalf("配置协调器应能申请宿主授权: tenant=%q err=%v", port.issuedTenant, err)
	}
	forged := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "cn.example.attacker"}}
	if _, _, err := issue(context.Background(), forged, issuePayload); err == nil {
		t.Fatal("其他插件不得申请配置授权")
	}

	consume := kernelConfigurationAuthorityConsume(port)
	token := configurationauthority.TokenPrefix + strings.Repeat("c", 64)
	custodian := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: configurationauthority.CustodianPluginID}}
	if _, _, err := consume(context.Background(), custodian, []byte(`{"token":"`+token+`"}`)); err != nil || port.consumedTenant != "tenant-a" || port.consumedToken != token {
		t.Fatalf("凭证托管器应能消费配置授权: tenant=%q token=%q err=%v", port.consumedTenant, port.consumedToken, err)
	}
	if _, _, err := consume(context.Background(), coordinator, []byte(`{"token":"`+token+`"}`)); err == nil {
		t.Fatal("配置协调器不得自行消费并解释授权")
	}
}

type runtimeLeaseBroker struct {
	tenant   string
	identity runtimeidentity.Identity
}

func (b *runtimeLeaseBroker) IssueRuntimeLease(_ context.Context, tenant string, identity runtimeidentity.Identity, request credentiallease.Request) (credentiallease.Envelope, error) {
	b.tenant, b.identity = tenant, identity
	return credentiallease.Envelope{Version: 1, TenantID: tenant, Audience: "runtime:v1:test", Ref: request.Ref}, nil
}

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

func (o *deploymentReadinessObserver) Observe(_ context.Context, tenant, deployment string, revision uint64) (deploymentpublication.ReadinessObservation, error) {
	o.called = tenant == "tenant-a" && deployment == "services" && revision == 9
	return deploymentpublication.ReadinessObservation{
		SchemaVersion: 1, Tenant: tenant, Deployment: deployment, Revision: revision,
		Status: deploymentpublication.ReadinessReady, UpdatedAt: time.Now().UTC(),
	}, nil
}

func (o *readinessObserver) Observe(_ context.Context, expectation nodebootstrap.ReadinessExpectation) (nodebootstrap.ReadinessObservation, error) {
	o.called = expectation.TenantID == "tenant-a" && expectation.NodeID == "node-a"
	return nodebootstrap.ReadinessObservation{Status: nodebootstrap.ReadinessReady}, nil
}

func (b *bootstrapBroker) Bootstrap(_ context.Context, scope nodebootstrap.Scope, plan nodebootstrap.Plan) (nodebootstrap.Result, error) {
	b.called = scope.TenantID == plan.Node.Tenant
	return nodebootstrap.Result{SystemdActive: true, NodeID: plan.Node.ID}, nil
}

func TestRuntimeMaterialLeaseRequiresHostOnlyLaunchIdentity(t *testing.T) {
	broker := &runtimeLeaseBroker{}
	handler := kernelRuntimeMaterialLease(broker)
	ref := commonv1.ManagedCredentialRef{
		Handle: "credential://managed/database", Scope: "tenant",
		Owner: "cn.vastplan.platform.data.relational.connection-manager", Purpose: "database.connection", Version: 1,
	}
	request, recipient, err := credentiallease.NewRequest(ref)
	if err != nil {
		t.Fatal(err)
	}
	defer recipient.Discard()
	payload, _ := json.Marshal(request)
	call := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{
		Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "cn.vastplan.foundation.data.relational.runtime",
	}}
	if _, _, err := handler(context.Background(), call, payload); err == nil {
		t.Fatal("只有 wire plugin caller、没有 host-only 启动身份时必须拒绝")
	}
	identity := runtimeidentity.Identity{
		PluginID: call.Caller.Id, Publisher: "vastplan", Version: "0.2.0", ArtifactSHA256: strings.Repeat("a", 64),
		NodeID: "node-a", RuntimeScope: "database", InstanceID: "runtime-a",
	}
	ctx, err := runtimeidentity.WithIdentity(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := handler(ctx, call, payload)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || broker.tenant != "tenant-a" || broker.identity != identity {
		t.Fatalf("可信 runtime lease 未转发: result=%+v broker=%+v err=%v", result, broker, err)
	}
	call.Caller.Id = "cn.vastplan.foundation.data.relational.other"
	if _, _, err := handler(ctx, call, payload); err == nil {
		t.Fatal("wire caller 与 host identity 不一致必须拒绝")
	}
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
	provider, err := kernelspi.NewPluginMapConfig(map[string]map[string]any{
		"plugin.a": {"retries": 3},
		"plugin.b": {"region": "cn-east"},
	})
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
	otherCtx := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "plugin.b"}}
	if _, _, err := service(context.Background(), otherCtx, []byte(`{"key":"retries"}`)); !errors.Is(err, kernelspi.ErrNotFound) {
		t.Fatalf("同一宿主内的其他插件不得读取 plugin.a 配置: %v", err)
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

func TestKernelDeploymentReadinessAcceptsOnlyDeploymentManager(t *testing.T) {
	observer := &deploymentReadinessObserver{}
	service := kernelDeploymentReadiness(observer)
	request, _ := json.Marshal(deploymentpublication.ReadinessRequest{DeploymentName: "services", DeploymentRevision: 9})
	trusted := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: deploymentpublication.DeploymentManagerPluginID}}
	result, raw, err := service(context.Background(), trusted, request)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || !observer.called || !strings.Contains(string(raw), `"status":"Ready"`) {
		t.Fatalf("deployment-manager readiness 查询失败: result=%+v raw=%s err=%v", result, raw, err)
	}
	untrusted := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "third.party"}}
	if _, _, err := service(context.Background(), untrusted, request); err == nil {
		t.Fatal("其他插件不得读取部署 readiness")
	}
}

func TestKernelConfigurationCatalogsAcceptOnlyPluginSettings(t *testing.T) {
	reader := &configurationCatalogReader{}
	service := kernelConfigurationCatalogs(reader)
	trusted := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: pluginconfiguration.PluginSettingsID}}
	result, raw, err := service(context.Background(), trusted, []byte(`{}`))
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || reader.tenant != "tenant-a" || string(raw) != `{"items":[]}` {
		t.Fatalf("plugin-settings 配置目录查询失败: result=%+v raw=%s err=%v", result, raw, err)
	}
	untrusted := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: deploymentpublication.DeploymentManagerPluginID}}
	if _, _, err := service(context.Background(), untrusted, []byte(`{}`)); err == nil {
		t.Fatal("其他插件不得读取内核配置目录")
	}
	if _, _, err := service(context.Background(), trusted, []byte(`{"tenant":"other"}`)); err == nil {
		t.Fatal("配置目录查询不得接受 payload tenant")
	}
}

func TestKernelManagedCredentialRefUsesAuthenticatedPluginIdentity(t *testing.T) {
	provider, err := kernelspi.NewPluginMapManagedCredentialRefs(map[string]map[string]pluginconfig.ManagedCredentialRef{
		"plugin.a": {"token": {Handle: "credential://managed/a", Scope: "tenant", Owner: "plugin.a", Purpose: "example.token", Version: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service := kernelManagedCredentialRef(provider)
	call := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "plugin.a"}}
	result, raw, err := service(context.Background(), call, []byte(`{"fieldId":"token"}`))
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || !strings.Contains(string(raw), `"owner":"plugin.a"`) {
		t.Fatalf("owner 插件应读取自己的托管引用: result=%+v raw=%s err=%v", result, raw, err)
	}
	call.Caller.Id = "plugin.b"
	if _, _, err := service(context.Background(), call, []byte(`{"fieldId":"token"}`)); !errors.Is(err, kernelspi.ErrNotFound) {
		t.Fatalf("其他插件不得读取托管引用: %v", err)
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
