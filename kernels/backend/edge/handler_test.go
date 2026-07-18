package edge

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	uiv1 "cdsoft.com.cn/VastPlan/schemas/ui/v1"
	"cdsoft.com.cn/VastPlan/shared/go/errorcode"
	"cdsoft.com.cn/VastPlan/shared/go/interactionapi"
	"cdsoft.com.cn/VastPlan/shared/go/portalapi"
)

type identity func(*http.Request) (portalapi.Principal, error)

func (f identity) Authenticate(r *http.Request) (portalapi.Principal, error) { return f(r) }

type service struct {
	seen      portalapi.Principal
	created   portalapi.PortalSpec
	createErr error
}

func (s *service) CreateDraft(_ context.Context, p portalapi.Principal, v portalapi.PortalSpec) (portalapi.Revision, error) {
	s.seen = p
	s.created = v
	if s.createErr != nil {
		return portalapi.Revision{}, s.createErr
	}
	return portalapi.Revision{ID: 1, TenantID: p.TenantID, PortalID: v.ID, Status: portalapi.StatusDraft}, nil
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
func (*service) List(context.Context, portalapi.Principal) ([]portalapi.Revision, error) {
	return nil, nil
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
	body := `{"id":"admin","route":"/","designSystem":{"id":"com.vastplan.foundation.frontend.design-system.arco","version":"1.0.0","uiContract":"^1.0.0"},"plugins":[{"id":"com.vastplan.foundation.frontend.design-system.arco","version":"1.0.0"}],"tenantId":"attacker"}`
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
