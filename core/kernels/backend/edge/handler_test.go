package edge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	uiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/ui/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/core/shared/go/errorcode"
	"cdsoft.com.cn/VastPlan/core/shared/go/interactionapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

type identity func(*http.Request) (portalapi.Principal, error)

func (f identity) Authenticate(r *http.Request) (portalapi.Principal, error) { return f(r) }

type service struct {
	seen        portalapi.Principal
	created     frontendcompositionv1.ApplicationComposition
	createErr   error
	revisions   []portalapi.Revision
	activations []portalapi.PortalActivation
	listErr     error
}

func (s *service) CreateDraft(_ context.Context, p portalapi.Principal, v frontendcompositionv1.ApplicationComposition) (portalapi.Revision, error) {
	s.seen = p
	s.created = v
	if s.createErr != nil {
		return portalapi.Revision{}, s.createErr
	}
	return portalapi.Revision{ID: 1, TenantID: p.TenantID, PortalID: v.ID, Status: portalapi.StatusDraft}, nil
}
func (s *service) UpdateDraft(_ context.Context, p portalapi.Principal, id uint64, v frontendcompositionv1.ApplicationComposition) (portalapi.Revision, error) {
	s.seen = p
	s.created = v
	return portalapi.Revision{ID: id, TenantID: p.TenantID, PortalID: v.ID, Status: portalapi.StatusDraft}, nil
}

func TestBFFMapsKernelPermissionDenialToForbidden(t *testing.T) {
	s := &service{createErr: &CapabilityError{Code: errorcode.PermissionDenied, Message: "缺少门户角色"}}
	h := New(identity(func(*http.Request) (portalapi.Principal, error) {
		return portalapi.Principal{ID: "reader", TenantID: "tenant-a", Roles: []string{"portal.read"}}, nil
	}), s)
	csrf := httptest.NewRequest(http.MethodGet, "/v1/csrf", nil)
	csrfW := httptest.NewRecorder()
	h.ServeHTTP(csrfW, csrf)
	cookie := csrfW.Result().Cookies()[0]
	r := httptest.NewRequest(http.MethodPost, "/v1/portal-drafts", strings.NewReader(`{}`))
	r.AddCookie(cookie)
	r.Header.Set("X-VastPlan-CSRF", cookie.Value)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("内核权限拒绝必须映射为 403: %d", w.Code)
	}
}
func (s *service) List(context.Context, portalapi.Principal) ([]portalapi.Revision, error) {
	return append([]portalapi.Revision(nil), s.revisions...), s.listErr
}
func (s *service) ListActivations(context.Context, portalapi.Principal) ([]portalapi.PortalActivation, error) {
	return append([]portalapi.PortalActivation(nil), s.activations...), s.listErr
}

func TestBFFServesOnlyVerifiedModulesFromActiveRevision(t *testing.T) {
	module := []byte(`export default { register() {} };`)
	dir := t.TempDir()
	manifest := `{"id":"cn.vastplan.foundation.frontend.render.adapter.test","name":"test","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/main.js"},"contributes":{"frontend":{"renderAdapters":[{"id":"ui.render.adapter","uiContract":"^4.0.0","framework":"test","capabilities":["layout","menu","overlay","form","data","feedback","theme"]}]}}}`
	if err := os.WriteFile(filepath.Join(dir, "vastplan.plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "frontend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frontend", "main.js"), module, 0o600); err != nil {
		t.Fatal(err)
	}
	pkg, _, err := pluginservice.PackageDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := pluginservice.Describe("stable", pkg)
	if err != nil {
		t.Fatal(err)
	}
	source := catalogSource{artifact.PluginID + "@" + artifact.Version: {Artifact: artifact, PackageBytes: pkg}}
	layoutArtifact, layoutPackage := packageFrontendFixture(t, `{"id":"cn.vastplan.foundation.frontend.structure.layout.test-runtime","name":"layout","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/main.js"},"contributes":{"frontend":{"shellLibraries":[{"id":"standard","shell":"ui.structure.shell","uiContract":"^4.0.0"}]}}}`, []byte(`export const shellLibrary = { id: "standard" };`))
	shellArtifact, shellPackage := packageFrontendFixture(t, fmt.Sprintf(`{"id":"cn.vastplan.foundation.frontend.structure.shell.test","name":"shell","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/main.js"},"contributes":{"frontend":{"shells":[{"id":"ui.structure.shell","uiContract":"^4.0.0","libraries":[{"id":"standard","module":{"id":%q,"version":"1.0.0","channel":"stable"}}]}]}}}`, layoutArtifact.PluginID), []byte(`export default { id: "ui.structure.shell" };`))
	workbenchArtifact, workbenchPackage := packageFrontendFixture(t, `{"id":"cn.vastplan.foundation.frontend.workflow.workbench.test","name":"workbench","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/main.js"},"contributes":{"frontend":{"workbenches":[{"id":"ui.workflow.workbench","uiContract":"^4.0.0"}]}}}`, []byte(`export default { id: "ui.workflow.workbench" };`))
	source[shellArtifact.PluginID+"@"+shellArtifact.Version] = artifacttrust.Envelope{Artifact: shellArtifact, PackageBytes: shellPackage}
	source[layoutArtifact.PluginID+"@"+layoutArtifact.Version] = artifacttrust.Envelope{Artifact: layoutArtifact, PackageBytes: layoutPackage}
	source[workbenchArtifact.PluginID+"@"+workbenchArtifact.Version] = artifacttrust.Envelope{Artifact: workbenchArtifact, PackageBytes: workbenchPackage}
	catalog, err := NewTrustedCatalog([]ArtifactSource{source}, contentVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	ref := portalapi.PluginRef{ID: artifact.PluginID, Version: artifact.Version}
	shellRef := portalapi.PluginRef{ID: shellArtifact.PluginID, Version: shellArtifact.Version}
	layoutRef := portalapi.PluginRef{ID: layoutArtifact.PluginID, Version: layoutArtifact.Version}
	workbenchRef := portalapi.PluginRef{ID: workbenchArtifact.PluginID, Version: workbenchArtifact.Version}
	spec := portalapi.PortalSpec{
		Revision: 7, ID: "admin", TenantID: "tenant-a", Route: "/", RenderAdapter: portalapi.RenderAdapter{PluginRef: ref, UIContract: "^4.0.0"}, Shell: portalapi.Shell{PluginRef: shellRef, UIContract: "^4.0.0", Config: frontendcompositionv1.ShellConfig{DefaultTemplate: "standard", AllowedTemplates: []string{"standard"}}}, Workbench: portalapi.Workbench{PluginRef: workbenchRef, UIContract: "^4.0.0"}, Plugins: []portalapi.PluginRef{ref, shellRef, layoutRef, workbenchRef},
		Resolution: portalapi.Resolution{PlatformProfile: compositioncommonv1.Ref{ID: "default", Revision: 1, Digest: strings.Repeat("a", 64)}, ApplicationComposition: compositioncommonv1.Ref{ID: "admin", Revision: 1, Digest: strings.Repeat("b", 64)}, PluginOrigins: map[string]string{ref.ID: compositioncommonv1.OriginPlatformProfile, shellRef.ID: compositioncommonv1.OriginPlatformProfile, layoutRef.ID: compositioncommonv1.OriginPlatformProfile, workbenchRef.ID: compositioncommonv1.OriginPlatformProfile}},
	}
	lockTestManagement(&spec)
	fallbackSpec := spec
	fallbackSpec.Revision = 6
	if err := catalog.MaterializePortal(context.Background(), "tenant-a", spec); err != nil {
		t.Fatal(err)
	}
	if err := catalog.MaterializePortal(context.Background(), "tenant-a", fallbackSpec); err != nil {
		t.Fatal(err)
	}
	s := &service{activations: []portalapi.PortalActivation{
		{ID: 7, TenantID: "tenant-a", PortalID: "admin", Status: portalapi.ActivationCurrent, Spec: spec},
		{ID: 6, TenantID: "tenant-a", PortalID: "admin", Status: portalapi.ActivationSuperseded, Spec: fallbackSpec},
	}}
	h := NewWithRuntime(identity(func(*http.Request) (portalapi.Principal, error) {
		return portalapi.Principal{ID: "reader", TenantID: "tenant-a", Roles: []string{"portal.read"}}, nil
	}), s, nil, catalog)

	runtimeRequest := httptest.NewRequest(http.MethodGet, "/v1/portal-runtime?path=/settings", nil)
	runtimeW := httptest.NewRecorder()
	h.ServeHTTP(runtimeW, runtimeRequest)
	if runtimeW.Code != http.StatusOK {
		t.Fatalf("读取运行描述 status=%d body=%s", runtimeW.Code, runtimeW.Body.String())
	}
	var runtime portalapi.RuntimeSpec
	if err := json.Unmarshal(runtimeW.Body.Bytes(), &runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.Portal.Revision != 7 || len(runtime.Modules) != 4 || !runtime.Modules[2].Deferred {
		t.Fatalf("运行描述未锁定当前 Activation: %+v", runtime)
	}
	links := runtimeW.Result().Header.Values("Link")
	if len(links) != len(runtime.Modules)-1 || !strings.Contains(links[0], "rel=preload; as=fetch; crossorigin=use-credentials") {
		t.Fatalf("模块预加载必须与认证 fetch 使用相同凭证模式: %v", links)
	}

	moduleRequest := httptest.NewRequest(http.MethodGet, runtime.Modules[0].URL, nil)
	moduleW := httptest.NewRecorder()
	h.ServeHTTP(moduleW, moduleRequest)
	if moduleW.Code != http.StatusOK || moduleW.Body.String() != string(module) || moduleW.Header().Get("X-VastPlan-Module-SHA256") != runtime.Modules[0].SHA256 {
		t.Fatalf("模块响应未绑定运行描述: status=%d headers=%v body=%s", moduleW.Code, moduleW.Header(), moduleW.Body.String())
	}
	notModified := httptest.NewRequest(http.MethodGet, runtime.Modules[0].URL, nil)
	notModified.Header.Set("If-None-Match", moduleW.Header().Get("ETag"))
	notModifiedW := httptest.NewRecorder()
	h.ServeHTTP(notModifiedW, notModified)
	if notModifiedW.Code != http.StatusNotModified || notModifiedW.Body.Len() != 0 {
		t.Fatalf("内容寻址模块应支持 304: status=%d", notModifiedW.Code)
	}

	missing := httptest.NewRequest(http.MethodGet, "/v1/portal-modules/8/"+runtime.Modules[0].SHA256+".js", nil)
	missingW := httptest.NewRecorder()
	h.ServeHTTP(missingW, missing)
	if missingW.Code != http.StatusNotFound {
		t.Fatalf("非当前 Activation 不得读取模块: %d", missingW.Code)
	}

	recoveryRequest := httptest.NewRequest(http.MethodGet, "/v1/portal-recovery?path=/settings", nil)
	recoveryW := httptest.NewRecorder()
	h.ServeHTTP(recoveryW, recoveryRequest)
	if recoveryW.Code != http.StatusOK {
		t.Fatalf("应返回服务端选择的安全恢复版本: status=%d body=%s", recoveryW.Code, recoveryW.Body.String())
	}
	var recovery portalapi.RuntimeSpec
	if err := json.Unmarshal(recoveryW.Body.Bytes(), &recovery); err != nil {
		t.Fatal(err)
	}
	if recovery.Portal.Revision != 6 || len(recovery.Modules) != 4 || recovery.Modules[0].URL != "/v1/portal-recovery-modules/7/6/"+recovery.Modules[0].SHA256+".js" {
		t.Fatalf("恢复描述未绑定 active/fallback revision: %+v", recovery)
	}
	recoveryModule := httptest.NewRequest(http.MethodGet, recovery.Modules[0].URL, nil)
	recoveryModuleW := httptest.NewRecorder()
	h.ServeHTTP(recoveryModuleW, recoveryModule)
	if recoveryModuleW.Code != http.StatusOK || recoveryModuleW.Body.String() != string(module) {
		t.Fatalf("读取恢复模块失败: status=%d body=%s", recoveryModuleW.Code, recoveryModuleW.Body.String())
	}
	tamperedRecovery := httptest.NewRequest(http.MethodGet, "/v1/portal-recovery-modules/7/5/"+recovery.Modules[0].SHA256+".js", nil)
	tamperedRecoveryW := httptest.NewRecorder()
	h.ServeHTTP(tamperedRecoveryW, tamperedRecovery)
	if tamperedRecoveryW.Code != http.StatusNotFound {
		t.Fatalf("浏览器指定非服务端选择的历史版本必须拒绝: %d", tamperedRecoveryW.Code)
	}
}
func (*service) Submit(context.Context, portalapi.Principal, uint64) (portalapi.Revision, error) {
	return portalapi.Revision{}, nil
}
func (*service) Approve(context.Context, portalapi.Principal, uint64) (portalapi.Revision, error) {
	return portalapi.Revision{}, nil
}
func (*service) Publish(context.Context, portalapi.Principal, uint64, portalapi.PublishRequest) (portalapi.Revision, error) {
	return portalapi.Revision{}, nil
}
func (*service) Audit(context.Context, portalapi.Principal, uint64) ([]portalapi.AuditEvent, error) {
	return nil, nil
}
func (*service) Governance(context.Context, portalapi.Principal) (portalapi.GovernanceSnapshot, error) {
	return portalapi.GovernanceSnapshot{}, nil
}
func (*service) CreateProfileDraft(context.Context, portalapi.Principal, frontendcompositionv1.PlatformProfile) (portalapi.PlatformProfileRevision, error) {
	return portalapi.PlatformProfileRevision{}, nil
}
func (*service) UpdateProfileDraft(context.Context, portalapi.Principal, uint64, frontendcompositionv1.PlatformProfile) (portalapi.PlatformProfileRevision, error) {
	return portalapi.PlatformProfileRevision{}, nil
}
func (*service) TransitionProfile(context.Context, portalapi.Principal, uint64, string) (portalapi.PlatformProfileRevision, error) {
	return portalapi.PlatformProfileRevision{}, nil
}
func (*service) CreateBindingDraft(context.Context, portalapi.Principal, portalapi.BindingDraftRequest) (portalapi.BindingRevision, error) {
	return portalapi.BindingRevision{}, nil
}
func (*service) UpdateBindingDraft(context.Context, portalapi.Principal, uint64, portalapi.BindingDraftRequest) (portalapi.BindingRevision, error) {
	return portalapi.BindingRevision{}, nil
}
func (*service) TransitionBinding(context.Context, portalapi.Principal, uint64, string) (portalapi.BindingRevision, error) {
	return portalapi.BindingRevision{}, nil
}
func (*service) Activate(context.Context, portalapi.Principal, portalapi.ActivationRequest) (portalapi.PortalActivation, error) {
	return portalapi.PortalActivation{}, nil
}
func (*service) RollbackActivation(context.Context, portalapi.Principal, uint64, uint64, string) (portalapi.PortalActivation, error) {
	return portalapi.PortalActivation{}, nil
}

type interactionService struct {
	principal portalapi.Principal
	presented string
	response  uiv1.InteractionResponse
}

type testReleasePortalService struct {
	service
	principal portalapi.Principal
	bindingID string
	binding   portalapi.PutTestTargetBindingRequest
}

func (s *testReleasePortalService) ListTestTargetBindings(context.Context, portalapi.Principal) ([]portalapi.TestTargetBinding, error) {
	return nil, nil
}
func (s *testReleasePortalService) PutTestTargetBinding(_ context.Context, p portalapi.Principal, id string, request portalapi.PutTestTargetBindingRequest) (portalapi.TestTargetBinding, error) {
	s.principal, s.bindingID, s.binding = p, id, request
	return portalapi.TestTargetBinding{ID: id, TenantID: p.TenantID, Scope: request.Scope, PortalID: request.PortalID, PluginID: request.PluginID, Enabled: request.Enabled}, nil
}
func (s *testReleasePortalService) ListTestReleases(context.Context, portalapi.Principal) ([]portalapi.TestRelease, error) {
	return nil, nil
}
func (s *testReleasePortalService) CreateTestRelease(context.Context, portalapi.Principal, portalapi.CreateTestReleaseRequest) (portalapi.TestRelease, error) {
	return portalapi.TestRelease{}, nil
}
func (s *testReleasePortalService) RollbackTestRelease(context.Context, portalapi.Principal, uint64) (portalapi.TestRelease, error) {
	return portalapi.TestRelease{}, nil
}

func (s *interactionService) List(_ context.Context, p portalapi.Principal) ([]interactionapi.Record, error) {
	s.principal = p
	return []interactionapi.Record{{Request: uiv1.InteractionRequest{ID: "interaction-0001"}, State: interactionapi.StateCreated}}, nil
}
func (s *interactionService) Get(_ context.Context, p portalapi.Principal, id string) (interactionapi.Record, error) {
	s.principal = p
	return interactionapi.Record{Request: uiv1.InteractionRequest{ID: id}, State: interactionapi.StateCreated}, nil
}
func (s *interactionService) Present(_ context.Context, p portalapi.Principal, id string) (interactionapi.Record, error) {
	s.principal, s.presented = p, id
	return interactionapi.Record{Request: uiv1.InteractionRequest{ID: id}, State: interactionapi.StatePresented}, nil
}
func (s *interactionService) Respond(_ context.Context, p portalapi.Principal, id string, response uiv1.InteractionResponse) (interactionapi.Record, error) {
	s.principal, s.response = p, response
	return interactionapi.Record{Request: uiv1.InteractionRequest{ID: id}, State: interactionapi.StateAnswered, Response: &response}, nil
}

func TestBFFRejectsMissingSessionAndCSRFBeforeCallingPortalService(t *testing.T) {
	s := &service{}
	h := New(identity(func(*http.Request) (portalapi.Principal, error) { return portalapi.Principal{}, errors.New("missing") }), s)
	r := httptest.NewRequest(http.MethodPost, "/v1/portal-drafts", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("无会话必须拒绝: %d", w.Code)
	}
	h = New(identity(func(*http.Request) (portalapi.Principal, error) {
		return portalapi.Principal{ID: "real-user", TenantID: "tenant-a", Roles: []string{"portal.compose"}}, nil
	}), s)
	r = httptest.NewRequest(http.MethodPost, "/v1/portal-drafts", strings.NewReader(`{}`))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("缺 CSRF 必须拒绝: %d", w.Code)
	}
}

func TestBFFUsesVerifiedPrincipalAndStrictCSRF(t *testing.T) {
	s := &service{}
	h := New(identity(func(*http.Request) (portalapi.Principal, error) {
		return portalapi.Principal{ID: "real-user", TenantID: "tenant-a", Roles: []string{"portal.compose"}}, nil
	}), s)
	csrf := httptest.NewRequest(http.MethodGet, "/v1/csrf", nil)
	csrfW := httptest.NewRecorder()
	h.ServeHTTP(csrfW, csrf)
	if csrfW.Code != http.StatusOK {
		t.Fatal(csrfW.Code)
	}
	cookie := csrfW.Result().Cookies()[0]
	body := `{"version":1,"revision":1,"id":"admin","target":{"kernel":"frontend"},"route":"/","plugins":[],"tenantId":"attacker"}`
	r := httptest.NewRequest(http.MethodPost, "/v1/portal-drafts", strings.NewReader(body))
	r.AddCookie(cookie)
	r.Header.Set("X-VastPlan-CSRF", cookie.Value)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("未知的 tenantId 字段必须拒绝，阻止伪造身份: %d", w.Code)
	}
	body = strings.Replace(body, `,"tenantId":"attacker"`, "", 1)
	r = httptest.NewRequest(http.MethodPost, "/v1/portal-drafts", strings.NewReader(body))
	r.AddCookie(cookie)
	r.Header.Set("X-VastPlan-CSRF", cookie.Value)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || s.seen.ID != "real-user" || s.seen.TenantID != "tenant-a" {
		t.Fatalf("必须使用服务端验证的 Principal: status=%d principal=%+v", w.Code, s.seen)
	}
	if !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("CSRF cookie 安全属性错误: %+v", cookie)
	}
}

func TestBFFFrontendTestTargetUsesVerifiedPrincipalAndStrictResourceID(t *testing.T) {
	service := &testReleasePortalService{}
	h := New(identity(func(*http.Request) (portalapi.Principal, error) {
		return portalapi.Principal{ID: "portal-admin", TenantID: "tenant-a", Roles: []string{"portal.compose"}}, nil
	}), service)
	csrfRequest := httptest.NewRequest(http.MethodGet, "/v1/csrf", nil)
	csrfW := httptest.NewRecorder()
	h.ServeHTTP(csrfW, csrfRequest)
	cookie := csrfW.Result().Cookies()[0]
	body := `{"scope":"application-plugin","portalId":"admin","pluginId":"cn.vastplan.product.frontend.admin","allowedPublishers":["vastplan"],"enabled":true,"ifVersion":0}`
	request := httptest.NewRequest(http.MethodPut, "/v1/portal-governance/test-target-bindings/1-admin", strings.NewReader(body))
	request.AddCookie(cookie)
	request.Header.Set("X-VastPlan-CSRF", cookie.Value)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, request)
	if w.Code != http.StatusOK || service.principal.ID != "portal-admin" || service.principal.TenantID != "tenant-a" || service.bindingID != "1-admin" || service.binding.PluginID != "cn.vastplan.product.frontend.admin" {
		t.Fatalf("Frontend 测试目标 BFF 边界错误: status=%d principal=%+v id=%q binding=%+v", w.Code, service.principal, service.bindingID, service.binding)
	}
	bad := httptest.NewRequest(http.MethodPut, "/v1/portal-governance/test-target-bindings/../admin", strings.NewReader(body))
	bad.AddCookie(cookie)
	bad.Header.Set("X-VastPlan-CSRF", cookie.Value)
	badW := httptest.NewRecorder()
	h.ServeHTTP(badW, bad)
	if badW.Code != http.StatusNotFound {
		t.Fatalf("路径型资源 ID 必须拒绝: %d", badW.Code)
	}
}

func TestBFFInteractionEndpointsUseVerifiedPrincipalAndWebOnlySurface(t *testing.T) {
	portal := &service{}
	interactions := &interactionService{}
	h := NewWithInteraction(identity(func(*http.Request) (portalapi.Principal, error) {
		return portalapi.Principal{ID: "verified-user", TenantID: "tenant-a", Roles: []string{"approver"}}, nil
	}), portal, interactions)

	list := httptest.NewRequest(http.MethodGet, "/v1/interactions", nil)
	listW := httptest.NewRecorder()
	h.ServeHTTP(listW, list)
	if listW.Code != http.StatusOK || interactions.principal.ID != "verified-user" || interactions.principal.TenantID != "tenant-a" {
		t.Fatalf("读取交互必须使用服务端 Principal: status=%d principal=%+v", listW.Code, interactions.principal)
	}

	csrfRequest := httptest.NewRequest(http.MethodGet, "/v1/csrf", nil)
	csrfW := httptest.NewRecorder()
	h.ServeHTTP(csrfW, csrfRequest)
	cookie := csrfW.Result().Cookies()[0]
	present := httptest.NewRequest(http.MethodPost, "/v1/interactions/interaction-0001/present", strings.NewReader(`{}`))
	present.AddCookie(cookie)
	present.Header.Set("X-VastPlan-CSRF", cookie.Value)
	presentW := httptest.NewRecorder()
	h.ServeHTTP(presentW, present)
	if presentW.Code != http.StatusOK || interactions.presented != "interaction-0001" {
		t.Fatalf("呈现交互应经 CSRF 保护后调用: status=%d id=%q", presentW.Code, interactions.presented)
	}

	// 交互端点只接受 response 本体；tenant、surface 等浏览器不应有权决定的
	// 字段在 Edge JSON 边界被拒绝，而 Web surface 由服务端固定注入。
	body := `{"interactionId":"interaction-0001","decision":"answered","tenantId":"attacker","surface":"mobile"}`
	respond := httptest.NewRequest(http.MethodPost, "/v1/interactions/interaction-0001/respond", strings.NewReader(body))
	respond.AddCookie(cookie)
	respond.Header.Set("X-VastPlan-CSRF", cookie.Value)
	respondW := httptest.NewRecorder()
	h.ServeHTTP(respondW, respond)
	if respondW.Code != http.StatusBadRequest || interactions.response.InteractionID != "" {
		t.Fatalf("浏览器不得伪造交互 tenant/surface: status=%d response=%+v", respondW.Code, interactions.response)
	}

	body = `{"interactionId":"interaction-0001","decision":"answered"}`
	respond = httptest.NewRequest(http.MethodPost, "/v1/interactions/interaction-0001/respond", strings.NewReader(body))
	respond.AddCookie(cookie)
	respond.Header.Set("X-VastPlan-CSRF", cookie.Value)
	respondW = httptest.NewRecorder()
	h.ServeHTTP(respondW, respond)
	if respondW.Code != http.StatusOK || interactions.response.InteractionID != "interaction-0001" || interactions.principal.ID != "verified-user" {
		t.Fatalf("交互响应应经验证主体转发: status=%d response=%+v principal=%+v", respondW.Code, interactions.response, interactions.principal)
	}
}
