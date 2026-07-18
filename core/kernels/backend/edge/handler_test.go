package edge

import (
	"context"
	"encoding/json"
	"errors"
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
	"cdsoft.com.cn/VastPlan/core/shared/go/errorcode"
	"cdsoft.com.cn/VastPlan/core/shared/go/interactionapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

type identity func(*http.Request) (portalapi.Principal, error)

func (f identity) Authenticate(r *http.Request) (portalapi.Principal, error) { return f(r) }

type service struct {
	seen      portalapi.Principal
	created   frontendcompositionv1.ApplicationComposition
	createErr error
	revisions []portalapi.Revision
	listErr   error
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

func TestBFFServesOnlyVerifiedModulesFromActiveRevision(t *testing.T) {
	module := []byte(`export default { register() {} };`)
	dir := t.TempDir()
	manifest := `{"id":"com.vastplan.foundation.frontend.design-system.test","name":"test","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/main.js"},"contributes":{"frontend":{"designSystems":[{"id":"ui.design-system","uiContract":"^1.0.0","framework":"test","capabilities":["layout","menu","overlay","form","data","feedback","theme"]}]}}}`
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
	catalog, err := NewTrustedCatalog([]ArtifactSource{source}, contentVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	ref := portalapi.PluginRef{ID: artifact.PluginID, Version: artifact.Version}
	spec := portalapi.PortalSpec{
		Revision: 7, ID: "admin", TenantID: "tenant-a", Route: "/", DesignSystem: portalapi.DesignSystem{PluginRef: ref, UIContract: "^1.0.0"}, Plugins: []portalapi.PluginRef{ref},
		Resolution: portalapi.Resolution{PlatformProfile: compositioncommonv1.Ref{ID: "default", Revision: 1, Digest: strings.Repeat("a", 64)}, ApplicationComposition: compositioncommonv1.Ref{ID: "admin", Revision: 1, Digest: strings.Repeat("b", 64)}, PluginOrigins: map[string]string{ref.ID: compositioncommonv1.OriginPlatformProfile}},
	}
	fallbackSpec := spec
	fallbackSpec.Revision = 6
	s := &service{revisions: []portalapi.Revision{
		{ID: 7, TenantID: "tenant-a", PortalID: "admin", Status: portalapi.StatusPublished, Active: true, Spec: spec},
		{ID: 6, TenantID: "tenant-a", PortalID: "admin", Status: portalapi.StatusPublished, Active: false, Spec: fallbackSpec},
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
	if runtime.Portal.Revision != 7 || len(runtime.Modules) != 1 {
		t.Fatalf("运行描述未锁定 active revision: %+v", runtime)
	}

	moduleRequest := httptest.NewRequest(http.MethodGet, runtime.Modules[0].URL, nil)
	moduleW := httptest.NewRecorder()
	h.ServeHTTP(moduleW, moduleRequest)
	if moduleW.Code != http.StatusOK || moduleW.Body.String() != string(module) || moduleW.Header().Get("X-VastPlan-Module-SHA256") != runtime.Modules[0].SHA256 {
		t.Fatalf("模块响应未绑定运行描述: status=%d headers=%v body=%s", moduleW.Code, moduleW.Header(), moduleW.Body.String())
	}

	missing := httptest.NewRequest(http.MethodGet, "/v1/portal-modules/8/"+ref.ID+".js", nil)
	missingW := httptest.NewRecorder()
	h.ServeHTTP(missingW, missing)
	if missingW.Code != http.StatusNotFound {
		t.Fatalf("非 active revision 不得读取模块: %d", missingW.Code)
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
	if recovery.Portal.Revision != 6 || len(recovery.Modules) != 1 || recovery.Modules[0].URL != "/v1/portal-recovery-modules/7/6/"+ref.ID+".js" {
		t.Fatalf("恢复描述未绑定 active/fallback revision: %+v", recovery)
	}
	recoveryModule := httptest.NewRequest(http.MethodGet, recovery.Modules[0].URL, nil)
	recoveryModuleW := httptest.NewRecorder()
	h.ServeHTTP(recoveryModuleW, recoveryModule)
	if recoveryModuleW.Code != http.StatusOK || recoveryModuleW.Body.String() != string(module) {
		t.Fatalf("读取恢复模块失败: status=%d body=%s", recoveryModuleW.Code, recoveryModuleW.Body.String())
	}
	tamperedRecovery := httptest.NewRequest(http.MethodGet, "/v1/portal-recovery-modules/7/5/"+ref.ID+".js", nil)
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
func (*service) Rollback(context.Context, portalapi.Principal, uint64, portalapi.PublishRequest) (portalapi.Revision, error) {
	return portalapi.Revision{}, nil
}
func (*service) Audit(context.Context, portalapi.Principal, uint64) ([]portalapi.AuditEvent, error) {
	return nil, nil
}

type interactionService struct {
	principal portalapi.Principal
	presented string
	response  uiv1.InteractionResponse
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
