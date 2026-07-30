package versionledger

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

func TestServiceRoutesByTrustedNamespaceAndProjectsActor(t *testing.T) {
	primary := NewMemoryProvider()
	portal := NewMemoryProvider()
	registry := NewRegistry()
	if err := registry.Register("primary", primary); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("portal", portal); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "primary", []ProviderRoute{{Namespace: "portal.configuration", Provider: "portal"}})
	if err != nil {
		t.Fatal(err)
	}
	request := versioningv1.PutVersionRequest{
		Stream:         versioningv1.StreamKey{Namespace: "portal.configuration", StreamID: "portal-main"},
		IdempotencyKey: "portal-main:revision:0001", Content: json.RawMessage(`{"layout":"standard"}`),
	}
	call := pluginCall("tenant-a")
	result, raw := invokeService(t, service, versioningv1.OperationPutVersion, call, request)
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("写入失败: %+v", result)
	}
	parsed, err := versioningv1.ParseResult(versioningv1.OperationPutVersion, raw)
	if err != nil {
		t.Fatal(err)
	}
	version := parsed.(*versioningv1.PutVersionResult).Version
	if version.ActorID != "plugin:cn.vastplan.platform.configuration.portal-composer" {
		t.Fatalf("actor 未从可信 CallContext 投影: %s", version.ActorID)
	}
	if _, err := portal.GetVersion(context.Background(), Scope{TenantID: "tenant-a"}, versioningv1.GetVersionRequest{Ref: version.Ref}); err != nil {
		t.Fatalf("namespace 路由未写入 portal Provider: %v", err)
	}
	if _, err := primary.GetVersion(context.Background(), Scope{TenantID: "tenant-a"}, versioningv1.GetVersionRequest{Ref: version.Ref}); errorCode(err) != versioningv1.ErrorNotFound {
		t.Fatalf("namespace 路由不应写入默认 Provider: %v", err)
	}
}

func TestServiceRejectsDirectUserAndInvalidConfiguration(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register("primary", NewMemoryProvider()); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(registry, "missing", nil); err == nil {
		t.Fatal("默认 Provider 必须已注册")
	}
	if _, err := NewService(registry, "primary", []ProviderRoute{{Namespace: "portal.configuration", Provider: "missing"}}); err == nil {
		t.Fatal("route 必须引用已注册 Provider")
	}
	service, err := NewService(registry, "primary", nil)
	if err != nil {
		t.Fatal(err)
	}
	call := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "user-a"}}
	result, _ := invokeService(t, service, versioningv1.OperationProviders, call, versioningv1.ProviderListRequest{})
	if result.GetStatus() != contractv1.CallResult_STATUS_ERROR || result.GetError().GetCode() != versioningv1.ErrorInvalidRequest {
		t.Fatalf("用户不得直接调用基础 Version Ledger: %+v", result)
	}
}

func TestServiceContributionExposesCompleteProtocol(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register("primary", NewMemoryProvider()); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "primary", nil)
	if err != nil {
		t.Fatal(err)
	}
	contribution := service.Contribution()
	if contribution.ID != versioningv1.LedgerCapability || !json.Valid(contribution.Descriptor) || len(contribution.Handlers) != 6 {
		t.Fatalf("Version Ledger contribution 无效: %+v", contribution)
	}
	for _, operation := range []string{
		versioningv1.OperationProviders, versioningv1.OperationPutVersion, versioningv1.OperationGetVersion,
		versioningv1.OperationListHistory, versioningv1.OperationGetHead, versioningv1.OperationMoveHead,
	} {
		if contribution.Handlers[operation] == nil {
			t.Fatalf("Version Ledger 缺少 %s handler", operation)
		}
	}
}

type wrongVersionProvider struct{ *MemoryProvider }

func (p wrongVersionProvider) GetVersion(ctx context.Context, scope Scope, request versioningv1.GetVersionRequest) (versioningv1.GetVersionResult, error) {
	result, err := p.MemoryProvider.GetVersion(ctx, scope, request)
	if err == nil {
		result.Version.Ref.VersionID = strings.Repeat("f", 64)
	}
	return result, err
}

func TestServiceRejectsValidButMisroutedProviderResponse(t *testing.T) {
	provider := wrongVersionProvider{MemoryProvider: NewMemoryProvider()}
	registry := NewRegistry()
	if err := registry.Register("primary", provider); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "primary", nil)
	if err != nil {
		t.Fatal(err)
	}
	call := pluginCall("tenant-a")
	put := versioningv1.PutVersionRequest{
		Stream:         versioningv1.StreamKey{Namespace: "portal.configuration", StreamID: "portal-main"},
		IdempotencyKey: "portal-main:revision:0001", Content: json.RawMessage(`{}`),
	}
	result, raw := invokeService(t, service, versioningv1.OperationPutVersion, call, put)
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatal(result)
	}
	parsed, err := versioningv1.ParseResult(versioningv1.OperationPutVersion, raw)
	if err != nil {
		t.Fatal(err)
	}
	ref := parsed.(*versioningv1.PutVersionResult).Version.Ref
	result, _ = invokeService(t, service, versioningv1.OperationGetVersion, call, versioningv1.GetVersionRequest{Ref: ref})
	if result.GetStatus() != contractv1.CallResult_STATUS_ERROR || result.GetError().GetCode() != versioningv1.ErrorCorrupted {
		t.Fatalf("答非所问的 Provider 响应必须 fail-closed: %+v", result)
	}
}

func pluginCall(tenant string) *contractv1.CallContext {
	return &contractv1.CallContext{TenantId: tenant, Caller: &contractv1.Caller{
		Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "cn.vastplan.platform.configuration.portal-composer",
	}}
}

func invokeService(t *testing.T, service *Service, operation string, call *contractv1.CallContext, request any) (*contractv1.CallResult, []byte) {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	result, payload, err := service.Contribution().Handlers[operation](context.Background(), nil, call, raw)
	if err != nil {
		t.Fatal(err)
	}
	return result, payload
}
