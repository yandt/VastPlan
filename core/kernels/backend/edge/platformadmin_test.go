package edge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

type platformService struct {
	principal portalapi.Principal
	secret    string
}

func (s *platformService) ListSettings(_ context.Context, p portalapi.Principal, _ string) ([]platformadminapi.Setting, error) {
	s.principal = p
	return []platformadminapi.Setting{{Key: "portal.title", Value: json.RawMessage(`"VastPlan"`), Version: 2}}, nil
}
func (s *platformService) PutSetting(_ context.Context, p portalapi.Principal, key string, request platformadminapi.PutSettingRequest) (platformadminapi.Setting, error) {
	s.principal = p
	return platformadminapi.Setting{Key: key, Value: request.Value, Version: 3}, nil
}
func (*platformService) DeleteSetting(context.Context, portalapi.Principal, string, *int64) error {
	return nil
}
func (s *platformService) ListCredentials(_ context.Context, p portalapi.Principal, _ string) ([]platformadminapi.CredentialMetadata, error) {
	s.principal = p
	return []platformadminapi.CredentialMetadata{}, nil
}
func (s *platformService) PutCredential(_ context.Context, p portalapi.Principal, name string, request platformadminapi.PutCredentialRequest) (platformadminapi.CredentialMetadata, error) {
	s.principal, s.secret = p, request.Value
	return platformadminapi.CredentialMetadata{Name: name, Version: 1}, nil
}
func (*platformService) RotateCredential(context.Context, portalapi.Principal, string) (platformadminapi.CredentialMetadata, error) {
	return platformadminapi.CredentialMetadata{}, nil
}
func (*platformService) RevokeCredential(context.Context, portalapi.Principal, string) (platformadminapi.CredentialMetadata, error) {
	return platformadminapi.CredentialMetadata{}, nil
}
func (*platformService) ListDatabaseConnections(context.Context, portalapi.Principal) ([]platformadminapi.DatabaseConnection, error) {
	return []platformadminapi.DatabaseConnection{}, nil
}
func (*platformService) PutDatabaseConnection(_ context.Context, _ portalapi.Principal, name string, value platformadminapi.DatabaseConnection) (platformadminapi.DatabaseConnection, error) {
	value.Name = name
	return value, nil
}
func (*platformService) DeleteDatabaseConnection(context.Context, portalapi.Principal, string) error {
	return nil
}
func (*platformService) ProbeDatabaseConnection(context.Context, portalapi.Principal, string) (platformadminapi.DatabaseProbe, error) {
	return platformadminapi.DatabaseProbe{Ready: true}, nil
}
func (*platformService) ArtifactRepositoryStatus(context.Context, portalapi.Principal) (platformadminapi.ArtifactRepositoryStatus, error) {
	return platformadminapi.ArtifactRepositoryStatus{Ready: true}, nil
}

func TestPlatformAdminBFFUsesVerifiedPrincipalAndRoles(t *testing.T) {
	admin := &platformService{}
	h := NewPlatformPortal(identity(func(*http.Request) (portalapi.Principal, error) {
		return portalapi.Principal{ID: "operator", TenantID: "tenant-a", Roles: []string{"platform.settings.read"}}, nil
	}), &service{}, nil, admin, nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/v1/platform/settings", nil)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK || admin.principal.ID != "operator" || admin.principal.TenantID != "tenant-a" {
		t.Fatalf("平台读取必须使用会话主体: status=%d principal=%+v", response.Code, admin.principal)
	}

	h = NewPlatformPortal(identity(func(*http.Request) (portalapi.Principal, error) {
		return portalapi.Principal{ID: "operator", TenantID: "tenant-a", Roles: []string{"portal.read"}}, nil
	}), &service{}, nil, admin, nil, nil)
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
	}), &service{}, nil, admin, nil, nil)
	body := `{"value":"top-secret"}`
	request := httptest.NewRequest(http.MethodPut, "/v1/platform/credentials/database.main", strings.NewReader(body))
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 必须拒绝凭证写入: %d", response.Code)
	}

	csrfRequest := httptest.NewRequest(http.MethodGet, "/v1/csrf", nil)
	csrfResponse := httptest.NewRecorder()
	h.ServeHTTP(csrfResponse, csrfRequest)
	cookie := csrfResponse.Result().Cookies()[0]
	request = httptest.NewRequest(http.MethodPut, "/v1/platform/credentials/database.main", strings.NewReader(body))
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
	}), &service{}, nil, &platformService{}, nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/v1/platform/capabilities/platform.settings/list", nil)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("通用能力路径必须不存在: %d", response.Code)
	}
}

type recordingPlatformCaller struct {
	capability, operation string
	payload               []byte
}

func (c *recordingPlatformCaller) Call(_ context.Context, _ portalapi.Principal, capability, operation string, payload []byte) ([]byte, error) {
	c.capability, c.operation, c.payload = capability, operation, append([]byte(nil), payload...)
	return []byte(`{"items":[{"key":"x","value":true,"version":1,"updatedAt":"now"}]}`), nil
}

func TestCapabilityPlatformAdminServiceOwnsCapabilitySelection(t *testing.T) {
	caller := &recordingPlatformCaller{}
	service, err := NewCapabilityPlatformAdminService(caller)
	if err != nil {
		t.Fatal(err)
	}
	items, err := service.ListSettings(context.Background(), portalapi.Principal{ID: "u", TenantID: "t"}, "x")
	if err != nil || len(items) != 1 || caller.capability != platformadminapi.SettingsCapability || caller.operation != "list" {
		t.Fatalf("设置映射错误: items=%+v capability=%s operation=%s err=%v", items, caller.capability, caller.operation, err)
	}
}
