package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
	staging "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.versioning.content-staging/contentstaging"
)

func TestTicketedHTTPSUploadStreamsOnceAndCompletes(t *testing.T) {
	service := testUploadService(t)
	content := []byte("browser-streamed-content")
	created := beginUpload(t, service, content)
	configuration := testDataPlaneConfiguration()
	configuration.AllowedBrowserOrigins = []string{"https://portal.example"}
	tickets := newUploadTicketStore(*configuration, service)
	now := time.Now().UTC()
	tickets.now = func() time.Time { return now }
	token := "a" + strings.Repeat("b", 42)
	installation := apiv1.DataPlaneTicketInstallation{Ticket: token, Claims: apiv1.DataPlaneTicketClaims{
		TenantID: "tenant-a", PrincipalID: "alice", Mode: apiv1.ModeTicketRedirect, DataPlaneExposureID: configuration.Exposures[0].ExposureID, InstanceID: configuration.InstanceID,
		Method: http.MethodPut, Resource: "/v1/uploads/" + created.Upload.ID, ContentSHA256: created.Upload.ExpectedDigest, ExpiresAt: now.Add(30 * time.Second),
	}}
	raw, _ := json.Marshal(installation)
	if err := tickets.install(context.Background(), apiExposureCall(), raw); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("PUT /v1/uploads/{uploadId}", newUploadHandler(service, tickets, newUploadCORSPolicy(configuration)))
	request := httptest.NewRequest(http.MethodPut, "https://content.example/v1/uploads/"+created.Upload.ID+"?vp_ticket="+token, bytes.NewReader(content))
	request.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
	request.Header.Set("Origin", "https://portal.example")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "https://portal.example" {
		t.Fatalf("上传响应缺少精确 CORS Origin: %v", response.Header())
	}
	var streamed stagingv1.UploadStatusResult
	if err := json.Unmarshal(response.Body.Bytes(), &streamed); err != nil || streamed.Upload.State != stagingv1.StateUploading || streamed.Upload.ReceivedSize != int64(len(content)) {
		t.Fatalf("streamed=%+v err=%v", streamed, err)
	}
	replay := httptest.NewRequest(http.MethodPut, "https://content.example/v1/uploads/"+created.Upload.ID+"?vp_ticket="+token, bytes.NewReader(content))
	replay.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
	replayResponse := httptest.NewRecorder()
	mux.ServeHTTP(replayResponse, replay)
	if replayResponse.Code != http.StatusUnauthorized {
		t.Fatalf("一次性 Ticket 被重放: %d", replayResponse.Code)
	}
	completed := callStaging(t, service, stagingv1.OperationCompleteUpload, stagingv1.UploadRevisionRequest{UploadID: created.Upload.ID, ExpectedRevision: created.Upload.Revision})
	if completed.Upload.State != stagingv1.StateReady || completed.Content == nil || completed.Content.Digest != created.Upload.ExpectedDigest {
		t.Fatalf("complete=%+v", completed)
	}
}

func TestUploadTicketFailsClosedOnCallerBindingAndTLS(t *testing.T) {
	service := testUploadService(t)
	created := beginUpload(t, service, []byte("safe"))
	configuration := testDataPlaneConfiguration()
	tickets := newUploadTicketStore(*configuration, service)
	now := time.Now().UTC()
	tickets.now = func() time.Time { return now }
	installation := apiv1.DataPlaneTicketInstallation{Ticket: "a" + strings.Repeat("b", 42), Claims: apiv1.DataPlaneTicketClaims{
		TenantID: "tenant-a", PrincipalID: "alice", Mode: apiv1.ModeTicketRedirect, DataPlaneExposureID: configuration.Exposures[0].ExposureID, InstanceID: configuration.InstanceID,
		Method: http.MethodPut, Resource: "/v1/uploads/" + created.Upload.ID, ContentSHA256: created.Upload.ExpectedDigest, ExpiresAt: now.Add(30 * time.Second),
	}}
	raw, _ := json.Marshal(installation)
	attacker := apiExposureCall()
	attacker.Caller.Id = "cn.example.attacker"
	if err := tickets.install(context.Background(), attacker, raw); err == nil {
		t.Fatal("非 API Exposure 插件安装了 Ticket")
	}
	installation.Claims.PrincipalID = "bob"
	raw, _ = json.Marshal(installation)
	if err := tickets.install(context.Background(), apiExposureCall(), raw); err == nil {
		t.Fatal("其他主体为 alice 的 Upload 安装了 Ticket")
	}
	installation.Claims.PrincipalID = "alice"
	raw, _ = json.Marshal(installation)
	if err := tickets.install(context.Background(), apiExposureCall(), raw); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "http://content.example/v1/uploads/"+created.Upload.ID+"?vp_ticket="+installation.Ticket, bytes.NewReader([]byte("safe")))
	response := httptest.NewRecorder()
	request.SetPathValue("uploadId", created.Upload.ID)
	newUploadHandler(service, tickets, nil).ServeHTTP(response, request)
	if response.Code != http.StatusUpgradeRequired {
		t.Fatalf("明文上传未被拒绝: %d", response.Code)
	}
	secure := httptest.NewRequest(http.MethodPut, "https://content.example/v1/uploads/"+created.Upload.ID+"?vp_ticket="+installation.Ticket, bytes.NewReader([]byte("safe")))
	secure.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
	secureResponse := httptest.NewRecorder()
	secure.SetPathValue("uploadId", created.Upload.ID)
	newUploadHandler(service, tickets, nil).ServeHTTP(secureResponse, secure)
	if secureResponse.Code != http.StatusOK {
		t.Fatalf("明文请求不应消费 Ticket: %d body=%s", secureResponse.Code, secureResponse.Body.String())
	}
}

func TestTicketedHTTPSUploadRejectsChunkedOverflow(t *testing.T) {
	service := testUploadService(t)
	created := beginUpload(t, service, []byte("safe"))
	tickets, token := installUploadTicket(t, service, created)
	request := httptest.NewRequest(http.MethodPut, "https://content.example/v1/uploads/"+created.Upload.ID+"?vp_ticket="+token, bytes.NewReader([]byte("unsafe")))
	request.ContentLength = -1
	request.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
	response := httptest.NewRecorder()
	request.SetPathValue("uploadId", created.Upload.ID)
	newUploadHandler(service, tickets, nil).ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("chunked overflow status=%d body=%s", response.Code, response.Body.String())
	}
	var rejected stagingv1.UploadStatusResult
	if err := json.Unmarshal(response.Body.Bytes(), &rejected); err != nil || rejected.Upload.State != stagingv1.StateRejected || rejected.FailureCode != stagingv1.FailureSizeMismatch {
		t.Fatalf("rejected=%+v err=%v", rejected, err)
	}
}

func TestUploadCORSAllowsOnlyConfiguredPortalOrigin(t *testing.T) {
	configuration := &staging.DataPlaneConfiguration{
		Endpoint: "https://content.example", AllowedBrowserOrigins: []string{"https://portal.example"},
	}
	handler := newUploadHandler(nil, nil, newUploadCORSPolicy(configuration))
	allowed := httptest.NewRequest(http.MethodOptions, "https://content.example/v1/uploads/stg_abcdefghijklmnop?vp_ticket="+"a"+strings.Repeat("b", 42), nil)
	allowed.Header.Set("Origin", "https://portal.example")
	allowed.Header.Set("Access-Control-Request-Method", http.MethodPut)
	allowed.Header.Set("Access-Control-Request-Headers", "content-type")
	allowedResponse := httptest.NewRecorder()
	handler.ServeHTTP(allowedResponse, allowed)
	if allowedResponse.Code != http.StatusNoContent || allowedResponse.Header().Get("Access-Control-Allow-Origin") != "https://portal.example" {
		t.Fatalf("合法预检被拒绝: status=%d headers=%v", allowedResponse.Code, allowedResponse.Header())
	}
	denied := httptest.NewRequest(http.MethodOptions, "https://content.example/v1/uploads/stg_abcdefghijklmnop", nil)
	denied.Header.Set("Origin", "https://attacker.example")
	denied.Header.Set("Access-Control-Request-Method", http.MethodPut)
	deniedResponse := httptest.NewRecorder()
	handler.ServeHTTP(deniedResponse, denied)
	if deniedResponse.Code != http.StatusForbidden || deniedResponse.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("未配置 Origin 被放行: status=%d headers=%v", deniedResponse.Code, deniedResponse.Header())
	}
}

func TestPrivateUploadRequiresAllowedSPIFFEClient(t *testing.T) {
	allowed, _ := url.Parse("spiffe://vastplan/desktop/desktop-a")
	state := &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{URIs: []*url.URL{allowed}}}}}
	if !verifiedSPIFFEClient(state, []string{"spiffe://vastplan/desktop/"}) {
		t.Fatal("受信 Desktop SPIFFE 身份被拒绝")
	}
	if verifiedSPIFFEClient(state, []string{"spiffe://vastplan/backend/"}) || verifiedSPIFFEClient(&tls.ConnectionState{}, []string{"spiffe://vastplan/desktop/"}) {
		t.Fatal("未批准或未验证的 mTLS 身份被放行")
	}
}

func TestUploadLeaseRegistrarCachesHealthyLease(t *testing.T) {
	configuration := testDataPlaneConfiguration()
	configuration.Exposures = append(configuration.Exposures, staging.DataPlaneExposureBinding{TenantID: "tenant-b", ExposureID: "dpx_bbbbbbbbbbbbbbbbbbbb"})
	registrar := &uploadLeaseRegistrar{configuration: configuration}
	host := &uploadLeaseHost{}
	call := &contractv1.CallContext{TenantId: "tenant-a"}
	registrar.ensure(context.Background(), host, call)
	registrar.ensure(context.Background(), host, call)
	registrar.ensure(context.Background(), host, &contractv1.CallContext{TenantId: "tenant-b"})
	if host.calls != 2 || len(registrar.leases) != 2 || len(registrar.lastErrors) != 2 {
		t.Fatalf("EndpointLease 应按 tenant 缓存: calls=%d leases=%+v errors=%+v", host.calls, registrar.leases, registrar.lastErrors)
	}
}

type uploadLeaseHost struct{ calls int }

func (h *uploadLeaseHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	h.calls++
	if target.GetCapability() != apiExposureCapability || target.GetOperation() != "registerEndpointLease" {
		return nil, nil, errors.New("unexpected target")
	}
	var registration apiv1.EndpointLeaseRegistration
	if json.Unmarshal(payload, &registration) != nil || registration.Modes[0] != apiv1.ModeTicketRedirect {
		return nil, nil, errors.New("unexpected registration")
	}
	now := time.Now().UTC()
	lease := apiv1.EndpointLease{
		SchemaVersion: apiv1.SchemaVersion, LeaseID: "lease_" + "a2345678901234567890123456789012",
		DataPlaneExposureID: registration.DataPlaneExposureID, InstanceID: registration.InstanceID,
		Endpoint: registration.Endpoint, TLSIdentity: registration.TLSIdentity, Modes: registration.Modes,
		IssuedAt: now, ExpiresAt: now.Add(4 * time.Minute),
	}
	raw, _ := json.Marshal(lease)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func installUploadTicket(t *testing.T, service *staging.Service, created stagingv1.UploadStatusResult) (*uploadTicketStore, string) {
	t.Helper()
	configuration := *testDataPlaneConfiguration()
	tickets := newUploadTicketStore(configuration, service)
	now := time.Now().UTC()
	tickets.now = func() time.Time { return now }
	token := "a" + strings.Repeat("b", 42)
	installation := apiv1.DataPlaneTicketInstallation{Ticket: token, Claims: apiv1.DataPlaneTicketClaims{
		TenantID: "tenant-a", PrincipalID: "alice", Mode: apiv1.ModeTicketRedirect, DataPlaneExposureID: configuration.Exposures[0].ExposureID, InstanceID: configuration.InstanceID,
		Method: http.MethodPut, Resource: "/v1/uploads/" + created.Upload.ID, ContentSHA256: created.Upload.ExpectedDigest, ExpiresAt: now.Add(30 * time.Second),
	}}
	raw, _ := json.Marshal(installation)
	if err := tickets.install(context.Background(), apiExposureCall(), raw); err != nil {
		t.Fatal(err)
	}
	return tickets, token
}

func testDataPlaneConfiguration() *staging.DataPlaneConfiguration {
	return &staging.DataPlaneConfiguration{
		Listen: "127.0.0.1:9444", Endpoint: "https://content.internal:9444", InstanceID: "content-1",
		TLSIdentity: "spiffe://vastplan/content/content-1",
		Exposures:   []staging.DataPlaneExposureBinding{{TenantID: "tenant-a", ExposureID: "dpx_aaaaaaaaaaaaaaaaaaaa"}},
	}
}

func testUploadService(t *testing.T) *staging.Service {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	service, _, err := staging.BuildConfiguredService(context.Background(), staging.StartupConfiguration{
		Provider: staging.ProviderConfiguration{Protocol: staging.FileProviderProtocol, Root: root},
		Limits: staging.LimitConfiguration{
			MaxFileBytes: 1 << 20, MaxTenantBytes: 2 << 20, MaxTotalBytes: 4 << 20, MaxActiveUploadsPerTenant: 4,
			MaxLeaseSeconds: 300, MaxPreparedPerTenant: 8, PreparedProtectionSeconds: 3600, TerminalRetentionSeconds: 3600,
		},
		ReclaimIntervalSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func beginUpload(t *testing.T, service *staging.Service, content []byte) stagingv1.UploadStatusResult {
	t.Helper()
	sum := sha256.Sum256(content)
	request := stagingv1.BeginUploadRequest{
		SessionID: "ws_1234567890abcdef", ExpectedSessionRevision: 1, EnvironmentDigest: strings.Repeat("e", 64),
		Resource: resourcev1.ResourceKey{Type: "script.bundle", ID: "daily"}, Path: "main.bin", MediaType: "application/octet-stream",
		ExpectedDigest: hex.EncodeToString(sum[:]), ExpectedSize: int64(len(content)), LeaseSeconds: 300,
	}
	return callStaging(t, service, stagingv1.OperationBeginUpload, request)
}

func callStaging(t *testing.T, service *staging.Service, operation string, request any) stagingv1.UploadStatusResult {
	t.Helper()
	raw, _ := json.Marshal(request)
	idempotency := "upload-operation:0001"
	call := &contractv1.CallContext{
		TenantId: "tenant-a", IdempotencyKey: &idempotency, Principal: &contractv1.Principal{UserId: "alice"},
		Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "cn.vastplan.foundation.versioning.workspace"},
	}
	result, response, err := service.Contribution().Handlers[operation](context.Background(), nil, call, raw)
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		t.Fatalf("operation=%s result=%+v response=%s err=%v", operation, result, response, err)
	}
	parsed, err := stagingv1.ParseResult(operation, response)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func apiExposureCall() *contractv1.CallContext {
	return &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: apiExposurePluginID}}
}
