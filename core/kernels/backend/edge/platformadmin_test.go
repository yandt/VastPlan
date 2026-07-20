package edge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

type platformService struct {
	principal portalapi.Principal
	secret    string
}

func (s *platformService) ListSettings(_ context.Context, p portalapi.Principal, _ portalapi.ManagementTarget, _ string) ([]platformadminapi.Setting, error) {
	s.principal = p
	return []platformadminapi.Setting{{Key: "portal.title", Value: json.RawMessage(`"VastPlan"`), Version: 2}}, nil
}
func (s *platformService) PutSetting(_ context.Context, p portalapi.Principal, _ portalapi.ManagementTarget, key string, request platformadminapi.PutSettingRequest) (platformadminapi.Setting, error) {
	s.principal = p
	return platformadminapi.Setting{Key: key, Value: request.Value, Version: 3}, nil
}
func (*platformService) DeleteSetting(context.Context, portalapi.Principal, portalapi.ManagementTarget, string, *int64) error {
	return nil
}
func (s *platformService) ListCredentials(_ context.Context, p portalapi.Principal, _ portalapi.ManagementTarget, _ string) ([]platformadminapi.CredentialMetadata, error) {
	s.principal = p
	return []platformadminapi.CredentialMetadata{}, nil
}
func (s *platformService) PutCredential(_ context.Context, p portalapi.Principal, _ portalapi.ManagementTarget, name string, request platformadminapi.PutCredentialRequest) (platformadminapi.CredentialMetadata, error) {
	s.principal, s.secret = p, request.Value
	return platformadminapi.CredentialMetadata{Name: name, Version: 1}, nil
}
func (*platformService) RotateCredential(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (platformadminapi.CredentialMetadata, error) {
	return platformadminapi.CredentialMetadata{}, nil
}
func (*platformService) RevokeCredential(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (platformadminapi.CredentialMetadata, error) {
	return platformadminapi.CredentialMetadata{}, nil
}
func (*platformService) ListDatabaseConnections(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]platformadminapi.DatabaseConnection, error) {
	return []platformadminapi.DatabaseConnection{}, nil
}
func (*platformService) PutDatabaseConnection(_ context.Context, _ portalapi.Principal, _ portalapi.ManagementTarget, name string, value platformadminapi.PutDatabaseConnectionRequest) (platformadminapi.DatabaseConnection, error) {
	return platformadminapi.DatabaseConnection{Name: name, ProviderID: value.ProviderID, Endpoint: value.Endpoint, Database: value.Database}, nil
}
func (*platformService) DeleteDatabaseConnection(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) error {
	return nil
}
func (*platformService) ProbeDatabaseConnection(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (platformadminapi.DatabaseProbe, error) {
	return platformadminapi.DatabaseProbe{Ready: true}, nil
}
func (*platformService) ArtifactRepositoryStatus(context.Context, portalapi.Principal, portalapi.ManagementTarget) (platformadminapi.ArtifactRepositoryStatus, error) {
	return platformadminapi.ArtifactRepositoryStatus{Ready: true}, nil
}
func (*platformService) ListManagedNodes(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]platformadminapi.ManagedNode, error) {
	return []platformadminapi.ManagedNode{}, nil
}
func (*platformService) PutManagedNode(_ context.Context, _ portalapi.Principal, _ portalapi.ManagementTarget, id string, request platformadminapi.PutManagedNodeRequest) (platformadminapi.ManagedNode, error) {
	return platformadminapi.ManagedNode{ID: id, Plan: request.Plan, Version: 1}, nil
}
func (*platformService) ListBootstrapJobs(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]platformadminapi.BootstrapJob, error) {
	return []platformadminapi.BootstrapJob{}, nil
}
func (*platformService) CreateBootstrapJob(_ context.Context, _ portalapi.Principal, _ portalapi.ManagementTarget, nodeID string) (platformadminapi.BootstrapJob, error) {
	return platformadminapi.BootstrapJob{ID: "job-1", NodeID: nodeID, State: platformadminapi.BootstrapPending}, nil
}
func (*platformService) ApproveBootstrapJob(_ context.Context, _ portalapi.Principal, _ portalapi.ManagementTarget, jobID string) (platformadminapi.BootstrapJob, error) {
	return platformadminapi.BootstrapJob{ID: jobID, State: platformadminapi.BootstrapSystemdActive}, nil
}
func (*platformService) ListDeploymentTargets(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]platformadminapi.DeploymentTarget, error) {
	return []platformadminapi.DeploymentTarget{{DeploymentName: "agent-services"}}, nil
}
func (*platformService) ListServiceRevisions(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]platformadminapi.ServiceRevision, error) {
	return []platformadminapi.ServiceRevision{{ID: 1, Deployment: "agent-services", Status: platformadminapi.ServiceDraft}}, nil
}
func (*platformService) CreateServiceDraft(context.Context, portalapi.Principal, portalapi.ManagementTarget, platformadminapi.ServiceCompositionRequest) (platformadminapi.ServiceRevision, error) {
	return platformadminapi.ServiceRevision{ID: 1, Status: platformadminapi.ServiceDraft}, nil
}
func (*platformService) UpdateServiceDraft(_ context.Context, _ portalapi.Principal, _ portalapi.ManagementTarget, id uint64, _ platformadminapi.ServiceCompositionRequest) (platformadminapi.ServiceRevision, error) {
	return platformadminapi.ServiceRevision{ID: id, Status: platformadminapi.ServiceDraft}, nil
}
func (*platformService) SubmitServiceDraft(_ context.Context, _ portalapi.Principal, _ portalapi.ManagementTarget, id uint64) (platformadminapi.ServiceRevision, error) {
	return platformadminapi.ServiceRevision{ID: id, Status: platformadminapi.ServicePendingApproval}, nil
}
func (*platformService) ApproveServiceRevision(_ context.Context, _ portalapi.Principal, _ portalapi.ManagementTarget, id uint64) (platformadminapi.ServiceRevision, error) {
	return platformadminapi.ServiceRevision{ID: id, Status: platformadminapi.ServiceApproved}, nil
}
func (*platformService) PublishServiceRevision(_ context.Context, _ portalapi.Principal, _ portalapi.ManagementTarget, id uint64) (platformadminapi.ServiceRevision, error) {
	return platformadminapi.ServiceRevision{ID: id, Status: platformadminapi.ServicePublished, Active: true}, nil
}
func (*platformService) RollbackServiceRevision(_ context.Context, _ portalapi.Principal, _ portalapi.ManagementTarget, id uint64) (platformadminapi.ServiceRevision, error) {
	return platformadminapi.ServiceRevision{ID: id + 1, Status: platformadminapi.ServicePublished, Active: true}, nil
}
func (*platformService) ListServiceRevisionAudit(_ context.Context, _ portalapi.Principal, _ portalapi.ManagementTarget, id uint64) ([]platformadminapi.ServiceAuditEvent, error) {
	return []platformadminapi.ServiceAuditEvent{{ID: 1, RevisionID: id}}, nil
}
func (*platformService) ListTestTargetBindings(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]platformadminapi.TestTargetBinding, error) {
	return []platformadminapi.TestTargetBinding{{ID: "demo", Kind: platformadminapi.TestTargetBackend}}, nil
}
func (*platformService) PutTestTargetBinding(_ context.Context, _ portalapi.Principal, _ portalapi.ManagementTarget, id string, request platformadminapi.PutTestTargetBindingRequest) (platformadminapi.TestTargetBinding, error) {
	return platformadminapi.TestTargetBinding{ID: id, Kind: request.Kind, Deployment: request.Deployment, UnitID: request.UnitID, PluginID: request.PluginID}, nil
}
func (*platformService) ListTestReleases(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]platformadminapi.TestRelease, error) {
	return []platformadminapi.TestRelease{{ID: 1, Status: platformadminapi.TestReleaseReady}}, nil
}
func (*platformService) CreateTestRelease(_ context.Context, _ portalapi.Principal, _ portalapi.ManagementTarget, request platformadminapi.CreateTestReleaseRequest) (platformadminapi.TestRelease, error) {
	return platformadminapi.TestRelease{ID: 1, BindingID: request.BindingID, Artifact: request.Artifact, Status: platformadminapi.TestReleaseQueued}, nil
}
func (*platformService) RollbackTestRelease(_ context.Context, _ portalapi.Principal, _ portalapi.ManagementTarget, id uint64) (platformadminapi.TestRelease, error) {
	return platformadminapi.TestRelease{ID: id, Status: platformadminapi.TestReleaseRolledBack}, nil
}

func platformPortalService() *service {
	profile := compositioncommonv1.Ref{ID: "default", Revision: 1, Digest: strings.Repeat("a", 64)}
	binding := frontendcompositionv1.PortalBinding{TenantID: "tenant-a", PortalID: "operations", PlatformProfile: profile, Services: []frontendcompositionv1.ManagedService{
		{ID: "settings", LogicalService: "platform.settings", RoutingDomain: "platform", Capabilities: []frontendcompositionv1.CapabilityGrant{{Capability: platformadminapi.SettingsCapability, Read: []string{"list"}, Write: []string{"put", "delete"}}}},
		{ID: "credentials", LogicalService: "platform.credentials", RoutingDomain: "platform", Capabilities: []frontendcompositionv1.CapabilityGrant{{Capability: platformadminapi.CredentialsCapability, Read: []string{"list"}, Write: []string{"put", "rotate", "revoke"}}}},
		{ID: "database", LogicalService: "platform.database", RoutingDomain: "platform", Capabilities: []frontendcompositionv1.CapabilityGrant{{Capability: platformadminapi.DatabaseCapability, Read: []string{"list"}, Write: []string{"define", "remove", "probe"}}}},
		{ID: "artifacts", LogicalService: "platform.artifacts.repository", RoutingDomain: "platform", Capabilities: []frontendcompositionv1.CapabilityGrant{{Capability: platformadminapi.ArtifactsCapability, Read: []string{"status"}}}},
		{ID: "deployment", LogicalService: "platform.deployment", RoutingDomain: "platform", Capabilities: []frontendcompositionv1.CapabilityGrant{{Capability: platformadminapi.DeploymentCapability, Read: []string{"listNodes", "listBootstrapJobs", "listDeploymentTargets", "listServiceRevisions", "listServiceRevisionAudit", "listTestTargetBindings", "listTestReleases"}, Write: []string{"putNode", "createBootstrap", "approveBootstrap", "createServiceDraft", "updateServiceDraft", "submitServiceDraft", "approveServiceRevision", "publishServiceRevision", "rollbackServiceRevision", "putTestTargetBinding", "createTestRelease", "rollbackTestRelease"}}}},
	}}
	spec := portalapi.PortalSpec{Revision: 1, ID: "operations", TenantID: "tenant-a", Route: "/operations", Management: binding, Resolution: portalapi.Resolution{PlatformProfile: profile, ManagementBindingDigest: compositioncommonv1.Digest(binding)}}
	return &service{activations: []portalapi.PortalActivation{{ID: 1, TenantID: "tenant-a", PortalID: "operations", Status: portalapi.ActivationCurrent, Spec: spec}}}
}

func platformPath(serviceID, path string) string {
	return "/v1/portals/operations/platform/services/" + serviceID + "/" + path
}

func TestPlatformAdminBFFUsesVerifiedPrincipalAndRoles(t *testing.T) {
	admin := &platformService{}
	h := NewPlatformPortal(identity(func(*http.Request) (portalapi.Principal, error) {
		return portalapi.Principal{ID: "operator", TenantID: "tenant-a", Roles: []string{"platform.settings.read"}}, nil
	}), platformPortalService(), nil, admin, nil, nil)
	request := httptest.NewRequest(http.MethodGet, platformPath("settings", "settings"), nil)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK || admin.principal.ID != "operator" || admin.principal.TenantID != "tenant-a" {
		t.Fatalf("平台读取必须使用会话主体: status=%d principal=%+v", response.Code, admin.principal)
	}

	h = NewPlatformPortal(identity(func(*http.Request) (portalapi.Principal, error) {
		return portalapi.Principal{ID: "operator", TenantID: "tenant-a", Roles: []string{"portal.read"}}, nil
	}), platformPortalService(), nil, admin, nil, nil)
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("缺少平台角色必须在 Edge 拒绝: %d", response.Code)
	}
}

func TestPlatformAdminCredentialIsWriteOnlyAndCSRFProtected(t *testing.T) {
	admin := &platformService{}
	h := NewPlatformPortal(identity(func(*http.Request) (portalapi.Principal, error) {
		return portalapi.Principal{ID: "vault-admin", TenantID: "tenant-a", Roles: []string{"platform.credentials.write"}}, nil
	}), platformPortalService(), nil, admin, nil, nil)
	body := `{"value":"top-secret"}`
	request := httptest.NewRequest(http.MethodPut, platformPath("credentials", "credentials/database.main"), strings.NewReader(body))
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 必须拒绝凭证写入: %d", response.Code)
	}

	csrfRequest := httptest.NewRequest(http.MethodGet, "/v1/csrf", nil)
	csrfResponse := httptest.NewRecorder()
	h.ServeHTTP(csrfResponse, csrfRequest)
	cookie := csrfResponse.Result().Cookies()[0]
	request = httptest.NewRequest(http.MethodPut, platformPath("credentials", "credentials/database.main"), strings.NewReader(body))
	request.AddCookie(cookie)
	request.Header.Set("X-VastPlan-CSRF", cookie.Value)
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK || admin.secret != "top-secret" {
		t.Fatalf("凭证写入失败: status=%d secret=%q", response.Code, admin.secret)
	}
	if strings.Contains(response.Body.String(), "top-secret") {
		t.Fatal("凭证明文不得出现在响应中")
	}
}

func TestPlatformAdminDoesNotExposeGenericCapabilityProxy(t *testing.T) {
	if platformCapabilityAllowed(platformadminapi.SettingsCapability, "future") || platformCapabilityAllowed("product.agent.run", "invoke") {
		t.Fatal("白名单不得接受未知能力或操作")
	}
	h := NewPlatformPortal(identity(func(*http.Request) (portalapi.Principal, error) {
		return portalapi.Principal{ID: "admin", TenantID: "tenant-a", Roles: []string{"platform.admin"}}, nil
	}), platformPortalService(), nil, &platformService{}, nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/v1/platform/capabilities/platform.settings/list", nil)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("通用能力路径必须不存在: %d", response.Code)
	}
}

func TestPortalManagementBindingRejectsCrossServiceAndAudienceWidening(t *testing.T) {
	admin := &platformService{}
	portalService := platformPortalService()
	h := NewPlatformPortal(identity(func(*http.Request) (portalapi.Principal, error) {
		return portalapi.Principal{ID: "operator", TenantID: "tenant-a", Roles: []string{"platform.credentials.read"}}, nil
	}), portalService, nil, admin, nil, nil)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, platformPath("settings", "credentials"), nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("设置服务绑定不得跨界访问凭证 capability: %d", response.Code)
	}

	portalService.activations[0].Spec.Audience = []string{"portal.operations"}
	h = NewPlatformPortal(identity(func(*http.Request) (portalapi.Principal, error) {
		return portalapi.Principal{ID: "operator", TenantID: "tenant-a", Roles: []string{"platform.settings.read"}}, nil
	}), portalService, nil, admin, nil, nil)
	response = httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, platformPath("settings", "settings"), nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("未进入 Portal audience 不得调用其管理 API: %d", response.Code)
	}
}

func TestDeploymentRoutesAreRoleSeparatedAndAllowlisted(t *testing.T) {
	admin := &platformService{}
	h := NewPlatformPortal(identity(func(*http.Request) (portalapi.Principal, error) {
		return portalapi.Principal{ID: "operator", TenantID: "tenant-a", Roles: []string{"platform.deployment.read"}}, nil
	}), platformPortalService(), nil, admin, nil, nil)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, platformPath("deployment", "deployment/nodes"), nil))
	if response.Code != http.StatusOK {
		t.Fatalf("部署读取角色应可列出节点: %d", response.Code)
	}
	response = httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, platformPath("deployment", "deployment/test-target-bindings"), nil))
	if response.Code != http.StatusOK {
		t.Fatalf("部署读取角色应可列出测试目标绑定: %d", response.Code)
	}

	h = NewPlatformPortal(identity(func(*http.Request) (portalapi.Principal, error) {
		return portalapi.Principal{ID: "requester", TenantID: "tenant-a", Roles: []string{"platform.deployment.bootstrap"}}, nil
	}), platformPortalService(), nil, admin, nil, nil)
	response = httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, platformPath("deployment", "deployment/bootstrap-jobs"), nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("引导申请角色不能隐含读取权限: %d", response.Code)
	}
	if !platformCapabilityAllowed(platformadminapi.DeploymentCapability, "approveBootstrap") || !platformCapabilityAllowed(platformadminapi.DeploymentCapability, "rollbackTestRelease") || platformCapabilityAllowed(platformadminapi.DeploymentCapability, "shell") {
		t.Fatal("部署 capability 白名单必须固定操作且拒绝 shell")
	}
}

type recordingPlatformCaller struct {
	capability, operation string
	target                portalapi.ManagementTarget
	payload               []byte
}

func (c *recordingPlatformCaller) Call(_ context.Context, _ portalapi.Principal, target portalapi.ManagementTarget, capability, operation string, payload []byte) ([]byte, error) {
	c.target, c.capability, c.operation, c.payload = target, capability, operation, append([]byte(nil), payload...)
	return []byte(`{"items":[{"key":"x","value":true,"version":1,"updatedAt":"now"}]}`), nil
}

func TestCapabilityPlatformAdminServiceOwnsCapabilitySelection(t *testing.T) {
	caller := &recordingPlatformCaller{}
	service, err := NewCapabilityPlatformAdminService(caller)
	if err != nil {
		t.Fatal(err)
	}
	target := portalapi.ManagementTarget{Service: frontendcompositionv1.ManagedService{ID: "settings", LogicalService: "platform.settings.primary", RoutingDomain: "platform", Capabilities: []frontendcompositionv1.CapabilityGrant{{Capability: platformadminapi.SettingsCapability, Read: []string{"list"}}}}}
	items, err := service.ListSettings(context.Background(), portalapi.Principal{ID: "u", TenantID: "t"}, target, "x")
	if err != nil || len(items) != 1 || caller.capability != platformadminapi.SettingsCapability || caller.operation != "list" || caller.target.Service.LogicalService != "platform.settings.primary" {
		t.Fatalf("设置映射错误: items=%+v capability=%s operation=%s err=%v", items, caller.capability, caller.operation, err)
	}
}

func TestCapabilityPlatformAdminServiceRejectsReadOnlyBindingMutation(t *testing.T) {
	caller := &recordingPlatformCaller{}
	service, err := NewCapabilityPlatformAdminService(caller)
	if err != nil {
		t.Fatal(err)
	}
	target := portalapi.ManagementTarget{Service: frontendcompositionv1.ManagedService{ID: "settings", LogicalService: "platform.settings", RoutingDomain: "platform", Capabilities: []frontendcompositionv1.CapabilityGrant{{Capability: platformadminapi.SettingsCapability, Read: []string{"list"}}}}}
	_, err = service.PutSetting(context.Background(), portalapi.Principal{ID: "u", TenantID: "t"}, target, "portal.title", platformadminapi.PutSettingRequest{Value: json.RawMessage(`"title"`)})
	if !errors.Is(err, portalapi.ErrForbidden) || caller.capability != "" {
		t.Fatalf("只读绑定不得触发写 capability: caller=%s err=%v", caller.capability, err)
	}
}
