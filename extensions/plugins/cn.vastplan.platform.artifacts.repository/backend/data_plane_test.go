package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
)

func TestDataPlaneTicketIsInstalledByExactControlPlaneAndConsumedOnce(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store := newDataPlaneTicketStore("repo-1")
	store.now = func() time.Time { return now }
	installation := apiv1.DataPlaneTicketInstallation{Ticket: "a234567890123456789012345678901234567890123", Claims: apiv1.DataPlaneTicketClaims{
		TenantID: "tenant-a", PrincipalID: "alice", DataPlaneExposureID: "dpx_aaaaaaaaaaaaaaaaaaaa", InstanceID: "repo-1", Method: http.MethodGet, Resource: "/v1/artifacts/demo?channel=testing", ExpiresAt: now.Add(30 * time.Second),
	}}
	raw, _ := json.Marshal(installation)
	trusted := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "cn.vastplan.platform.integration.api-exposure"}}
	if err := store.install(trusted, raw); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://repo.example/v1/artifacts/demo?channel=testing&vp_ticket="+installation.Ticket, nil)
	if !store.consume(request) || request.URL.RawQuery != "channel=testing" {
		t.Fatalf("Ticket 应按资源绑定消费并从 query 移除: %s", request.URL.RawQuery)
	}
	request = httptest.NewRequest(http.MethodGet, "https://repo.example/v1/artifacts/demo?channel=testing&vp_ticket="+installation.Ticket, nil)
	if store.consume(request) {
		t.Fatal("Ticket 不得重复消费")
	}
	attacker := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "cn.example.attacker"}}
	if err := store.install(attacker, raw); err == nil {
		t.Fatal("非 API Exposure 插件不得安装 Ticket")
	}
}

func TestDataPlaneTicketMiddlewareInjectsOnlyInternalReadCredential(t *testing.T) {
	now := time.Now().UTC()
	store := newDataPlaneTicketStore("repo-1")
	store.now = func() time.Time { return now }
	token := "b234567890123456789012345678901234567890123"
	store.items[token] = apiv1.DataPlaneTicketClaims{TenantID: "tenant-a", PrincipalID: "alice", DataPlaneExposureID: "dpx_aaaaaaaaaaaaaaaaaaaa", InstanceID: "repo-1", Method: http.MethodGet, Resource: "/v1/artifacts/demo", ExpiresAt: now.Add(30 * time.Second)}
	handler := dataPlaneTicketMiddleware(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer private-reader" || request.URL.RawQuery != "" {
			t.Error("中间件未投影内部读取凭据或未移除 Ticket")
		}
		response.WriteHeader(http.StatusNoContent)
	}), store, "private-reader")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://repo.example/v1/artifacts/demo?vp_ticket="+token, nil))
	if response.Code != http.StatusNoContent || response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("Ticket 中间件响应错误: %+v", response)
	}
}

func TestLeaseRegistrarUsesPluginHostCallAndCachesHealthyLease(t *testing.T) {
	config := &dataPlaneLeaseConfig{ExposureID: "dpx_aaaaaaaaaaaaaaaaaaaa", InstanceID: "repo-1", Endpoint: "https://repo.internal:9443", TLSIdentity: "spiffe://vastplan/repository/repo-1"}
	registrar := &dataPlaneLeaseRegistrar{config: config}
	host := &leaseHost{}
	registrar.ensure(context.Background(), host, &contractv1.CallContext{})
	registrar.ensure(context.Background(), host, &contractv1.CallContext{})
	if host.calls != 1 || registrar.lease.LeaseID == "" {
		t.Fatalf("健康 Lease 应复用: calls=%d lease=%+v", host.calls, registrar.lease)
	}
}

type leaseHost struct{ calls int }

func (h *leaseHost) Call(_ context.Context, _ *contractv1.CallTarget, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
	h.calls++
	now := time.Now().UTC()
	lease := apiv1.EndpointLease{SchemaVersion: apiv1.SchemaVersion, LeaseID: "lease_" + "a2345678901234567890123456789012", DataPlaneExposureID: "dpx_aaaaaaaaaaaaaaaaaaaa", InstanceID: "repo-1", Endpoint: "https://repo.internal:9443", TLSIdentity: "spiffe://vastplan/repository/repo-1", Modes: []string{apiv1.ModeTicketRedirect}, IssuedAt: now, ExpiresAt: now.Add(4 * time.Minute)}
	raw, _ := json.Marshal(lease)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}
