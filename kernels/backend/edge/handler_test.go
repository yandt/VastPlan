package edge

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cdsoft.com.cn/VastPlan/shared/go/errorcode"
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
