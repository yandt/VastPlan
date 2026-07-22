package apiexposure

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestRuntimeDescriptorExactlyMatchesSignedManifest(t *testing.T) {
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
	for _, contribution := range contributions {
		if contribution.ExtensionPoint != "tool.package" || contribution.ID != Capability {
			continue
		}
		var expected, actual any
		if json.Unmarshal(contribution.Descriptor, &expected) != nil || json.Unmarshal(Descriptor(), &actual) != nil || !reflect.DeepEqual(expected, actual) {
			t.Fatalf("运行时 descriptor 与签名清单不一致\nexpected=%s\nactual=%s", contribution.Descriptor, Descriptor())
		}
		return
	}
	t.Fatal("签名清单缺少 API Exposure runtime contribution")
}

func TestHTTPExposureGovernancePublishesSelfContainedCatalogAndKeepsRouteKey(t *testing.T) {
	service, catalogFile := testService(t)
	creator := Principal{ID: "alice", TenantID: "tenant-a", Roles: []string{"platform.api-exposure.edit", "platform.api-exposure.read"}}
	approver := Principal{ID: "bob", TenantID: "tenant-a", Roles: []string{"platform.api-exposure.approve"}}
	publisher := Principal{ID: "carol", TenantID: "tenant-a", Roles: []string{"platform.api-exposure.publish"}}

	draft, err := service.CreateDraft(context.Background(), creator, CreateDraftRequest{Contract: testContractSelector(), Input: testExposureInput()})
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Exposure.RouteKey) != 20 || strings.Contains(draft.Exposure.RouteKey, "vastplan") {
		t.Fatalf("Route Key 必须是不透明随机值: %q", draft.Exposure.RouteKey)
	}
	pending, err := service.Transition(context.Background(), creator, draft.ID, "submit")
	if err != nil || pending.Status != StatusPendingApproval {
		t.Fatalf("提交失败: %+v %v", pending, err)
	}
	if _, err := service.Transition(context.Background(), Principal{ID: "alice", TenantID: "tenant-a", Roles: []string{"platform.api-exposure.approve"}}, draft.ID, "approve"); !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("自审批必须拒绝: %v", err)
	}
	if _, err := service.Transition(context.Background(), approver, draft.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	published, err := service.Transition(context.Background(), publisher, draft.ID, "publish")
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != StatusPublished {
		t.Fatalf("发布状态错误: %+v", published)
	}

	raw, err := os.ReadFile(catalogFile)
	if err != nil {
		t.Fatal(err)
	}
	var catalog apiv1.ExposureCatalog
	if json.Unmarshal(raw, &catalog) != nil || apiv1.ValidateExposureCatalog(catalog) != nil || len(catalog.Exposures) != 1 {
		t.Fatalf("Gateway Catalog 无效: %s", raw)
	}
	if catalog.Exposures[0].Contract.ContractID != "platform.demo.api" {
		t.Fatal("Gateway Catalog 必须自包含完整契约")
	}

	next, err := service.CreateDraft(context.Background(), creator, CreateDraftRequest{BaseExposureID: published.Exposure.ID, Contract: testContractSelector(), Input: testExposureInput()})
	if err != nil {
		t.Fatal(err)
	}
	if next.Exposure.RouteKey != published.Exposure.RouteKey || next.Exposure.ID != published.Exposure.ID || next.Exposure.Revision <= published.Exposure.Revision {
		t.Fatalf("实现换代必须保持公开身份: old=%+v next=%+v", published.Exposure, next.Exposure)
	}
}

func TestDataPlaneLeaseAndTicketAreBoundShortLivedAndSingleUse(t *testing.T) {
	service, catalogFile := testService(t)
	creator := Principal{ID: "alice", TenantID: "tenant-a", Roles: []string{"platform.api-exposure.edit"}}
	approver := Principal{ID: "bob", TenantID: "tenant-a", Roles: []string{"platform.api-exposure.approve"}}
	publisher := Principal{ID: "carol", TenantID: "tenant-a", Roles: []string{"platform.api-exposure.publish"}}
	reference := testDataPlaneReference()
	draft, err := service.CreateDataPlaneDraft(context.Background(), creator, CreateDataPlaneDraftRequest{Input: DataPlaneInput{
		Hosts: []string{"artifacts.example.com"}, Service: reference, AllowedModes: []string{apiv1.ModeTicketRedirect},
		AllowedEndpointOrigins: []string{"https://repository.internal:9443"}, TLSIdentityPrefix: "spiffe://vastplan/repository/",
		Authentication: apiv1.AuthenticationPolicy{ProfileID: "auth.file"}, RequiredPermissions: []string{"platform.artifacts.read"}, MaxObjectBytes: 1 << 30,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.TransitionDataPlane(context.Background(), creator, draft.ID, "submit"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.TransitionDataPlane(context.Background(), approver, draft.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	published, err := service.TransitionDataPlane(context.Background(), publisher, draft.ID, "publish")
	if err != nil {
		t.Fatal(err)
	}
	catalogRaw, err := os.ReadFile(catalogFile)
	if err != nil {
		t.Fatal(err)
	}
	var gatewayCatalog apiv1.ExposureCatalog
	if json.Unmarshal(catalogRaw, &gatewayCatalog) != nil || len(gatewayCatalog.DataPlaneExposures) != 1 || gatewayCatalog.DataPlaneExposures[0].RouteKey != published.Exposure.RouteKey {
		t.Fatalf("Gateway Catalog 缺少已发布 Data Plane Exposure: %s", catalogRaw)
	}

	caller := RuntimeCaller{PluginID: reference.PluginID, TenantID: "tenant-a"}
	if _, err := service.RegisterEndpointLease(context.Background(), caller, EndpointLeaseRequest{
		DataPlaneExposureID: published.Exposure.ID, InstanceID: "repository-1", Endpoint: "https://attacker.example", TLSIdentity: "spiffe://vastplan/repository/repository-1", Modes: []string{apiv1.ModeTicketRedirect}, TTLSeconds: 120,
	}); err == nil {
		t.Fatal("Endpoint Lease 不得越过已审批 origin")
	}
	lease, err := service.RegisterEndpointLease(context.Background(), caller, EndpointLeaseRequest{
		DataPlaneExposureID: published.Exposure.ID, InstanceID: "repository-1", Endpoint: "https://repository.internal:9443", TLSIdentity: "spiffe://vastplan/repository/repository-1", Modes: []string{apiv1.ModeTicketRedirect}, TTLSeconds: 120,
	})
	if err != nil {
		t.Fatal(err)
	}
	if lease.ExpiresAt.Sub(lease.IssuedAt) != 2*time.Minute {
		t.Fatalf("租期错误: %+v", lease)
	}
	grant, err := service.IssueTicket(context.Background(), Principal{ID: "reader", TenantID: "tenant-a", Roles: []string{"platform.artifacts.read"}}, TicketRequest{DataPlaneExposureID: published.Exposure.ID, Method: "GET", Resource: "/v1/artifacts/demo"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := service.ConsumeTicket(context.Background(), caller, TicketConsumption{Ticket: grant.Ticket, InstanceID: "repository-1"})
	if err != nil || claims.PrincipalID != "reader" || claims.Resource != "/v1/artifacts/demo" {
		t.Fatalf("Ticket claims 错误: %+v %v", claims, err)
	}
	if _, err := service.ConsumeTicket(context.Background(), caller, TicketConsumption{Ticket: grant.Ticket, InstanceID: "repository-1"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Ticket 必须单次消费: %v", err)
	}
	if _, err := service.RegisterEndpointLease(context.Background(), RuntimeCaller{PluginID: "cn.example.attacker", TenantID: "tenant-a"}, EndpointLeaseRequest{DataPlaneExposureID: published.Exposure.ID}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("非拥有插件不得登记 Lease: %v", err)
	}
	if err := service.RetireDataPlane(context.Background(), publisher, published.Exposure.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.IssueTicket(context.Background(), Principal{ID: "reader", TenantID: "tenant-a", Roles: []string{"platform.artifacts.read"}}, TicketRequest{DataPlaneExposureID: published.Exposure.ID, Method: "GET", Resource: "/v1/artifacts/demo"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("退役后不得继续签发 Ticket: %v", err)
	}
}

func TestRestartRecoversDirtyCatalogPublication(t *testing.T) {
	root := t.TempDir()
	stateFile, catalogFile := filepath.Join(root, "state.json"), filepath.Join(root, "catalog.json")
	service, err := New(stateFile, catalogFile, testContractCatalog())
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	service.state.CatalogDirty = true
	if err := service.saveLocked(); err != nil {
		t.Fatal(err)
	}
	service.mu.Unlock()
	if _, err := New(stateFile, catalogFile, testContractCatalog()); err != nil {
		t.Fatalf("重启应完成脏 Catalog 重放: %v", err)
	}
}

func TestRestartRemovesExposureWhoseArtifactIsNoLongerTrusted(t *testing.T) {
	root := t.TempDir()
	stateFile, catalogFile := filepath.Join(root, "state.json"), filepath.Join(root, "catalog.json")
	service, err := New(stateFile, catalogFile, testContractCatalog())
	if err != nil {
		t.Fatal(err)
	}
	creator := Principal{ID: "alice", TenantID: "tenant-a", Roles: []string{"platform.api-exposure.edit"}}
	draft, err := service.CreateDraft(context.Background(), creator, CreateDraftRequest{Contract: testContractSelector(), Input: testExposureInput()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Transition(context.Background(), creator, draft.ID, "submit"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Transition(context.Background(), Principal{ID: "bob", TenantID: "tenant-a", Roles: []string{"platform.api-exposure.approve"}}, draft.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Transition(context.Background(), Principal{ID: "carol", TenantID: "tenant-a", Roles: []string{"platform.api-exposure.publish"}}, draft.ID, "publish"); err != nil {
		t.Fatal(err)
	}
	if _, err := New(stateFile, catalogFile, EmptyContractCatalog()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(catalogFile)
	if err != nil {
		t.Fatal(err)
	}
	var catalog apiv1.ExposureCatalog
	if json.Unmarshal(raw, &catalog) != nil || len(catalog.Exposures) != 0 {
		t.Fatalf("失去信任的制品不得留在 Gateway Catalog: %s", raw)
	}
}

func TestLoadContractCatalogFileRejectsWritableOrSymlinkInput(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "catalog.json")
	raw, _ := json.Marshal(testContractCatalog())
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if catalog, err := LoadContractCatalogFile(path); err != nil || len(catalog.Contracts) != 1 {
		t.Fatalf("私有普通文件应可加载: %+v %v", catalog, err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadContractCatalogFile(path); err == nil {
		t.Fatal("可由其他用户写入的 Catalog 必须拒绝")
	}
	link := filepath.Join(root, "catalog-link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadContractCatalogFile(link); err == nil {
		t.Fatal("Catalog 符号链接必须拒绝")
	}
}

func testService(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	catalogFile := filepath.Join(root, "gateway-catalog.json")
	service, err := New(filepath.Join(root, "state.json"), catalogFile, testContractCatalog())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	return service, catalogFile
}

func testContractCatalog() apiv1.ContractCatalog {
	contract := apiv1.ContractContribution{ID: "management-api", ServiceRole: "backend", ContractID: "platform.demo.api", ContractVersion: "1.0.0", Protocol: apiv1.ProtocolHTTPJSON, Routes: []apiv1.RouteContract{{ID: "platform.demo.list", Method: "GET", Path: "/items", Target: apiv1.CapabilityTarget{Capability: "platform.demo", Operation: "list"}, RequestSchema: json.RawMessage(`{"type":"object"}`), ResponseSchema: json.RawMessage(`{"type":"object"}`), SuccessStatus: 200}}}
	digest, _ := apiv1.ContractDigest(contract)
	selector := testContractSelector()
	return apiv1.ContractCatalog{SchemaVersion: apiv1.SchemaVersion, Generation: 1, Contracts: []apiv1.ResolvedContract{{Reference: apiv1.ContractReference{PluginID: selector.PluginID, ArtifactSHA256: selector.ArtifactSHA256, ContributionID: selector.ContributionID, ContractID: contract.ContractID, ContractVersion: contract.ContractVersion, ContractDigest: digest}, Contract: contract}}, DataPlaneServices: []apiv1.ResolvedDataPlaneService{{Reference: testDataPlaneReference(), Service: apiv1.DataPlaneServiceContribution{ID: "artifact-data", ServiceRole: "backend", Protocol: "https", Purposes: []string{"artifact-download"}, SupportedModes: []string{apiv1.ModeTicketRedirect}, HealthPath: "/healthz", MaxObjectBytes: 1 << 30, TicketTarget: &apiv1.CapabilityTarget{Capability: "platform.artifacts.repository", Operation: "installDataPlaneTicket"}}}}}
}

func testContractSelector() ContractSelector {
	return ContractSelector{PluginID: "cn.vastplan.platform.demo", ArtifactSHA256: strings.Repeat("a", 64), ContributionID: "management-api"}
}
func testDataPlaneReference() apiv1.DataPlaneServiceReference {
	return apiv1.DataPlaneServiceReference{PluginID: "cn.vastplan.platform.artifacts.repository", ArtifactSHA256: strings.Repeat("b", 64), ContributionID: "artifact-data"}
}
func testExposureInput() ExposureInput {
	return ExposureInput{DisplayName: "演示 API", PortalID: "operations", Hosts: []string{"api.example.com"}, Authentication: apiv1.AuthenticationPolicy{ProfileID: "auth.file"}, RequiredPermissions: []string{"platform.demo.read"}, Limits: apiv1.ExposureLimits{MaxBodyBytes: 1024, MaxResponseBytes: 4096, RequestsPerMinute: 60, TimeoutMS: 5000}, Target: apiv1.ExposureTarget{LogicalService: "backend.default", RoutingDomain: "platform.default"}}
}
