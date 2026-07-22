package broker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
)

type staticCatalog struct {
	value authenticationv1.AuthenticationProviderCatalog
}

type testableCatalog struct{ staticCatalog }

func (testableCatalog) ResolveTestProfile(profileID, methodID string) (authenticationv1.ProviderCatalogEntry, bool, error) {
	if profileID != "corp-draft" || methodID != "corporate-sso" {
		return authenticationv1.ProviderCatalogEntry{}, false, nil
	}
	return authenticationv1.ProviderCatalogEntry{Profile: compositioncommonv1.Ref{ID: profileID, Revision: 1, Digest: strings.Repeat("e", 64)}, ContributionID: "oidc", Purposes: []authenticationv1.ProviderPurpose{authenticationv1.PurposePortalLogin}, Methods: []string{methodID}, SubjectNamespace: "enterprise.identity.oidc"}, true, nil
}

func (s staticCatalog) Load() (authenticationv1.AuthenticationProviderCatalog, error) {
	return s.value, nil
}

type fakeHost struct {
	calls []string
	now   time.Time
}

func (h *fakeHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	h.calls = append(h.calls, target.Capability+"/"+target.GetOperation())
	switch target.GetOperation() {
	case authenticationv1.OperationDescribe:
		return okResult(), mustJSON(authenticationv1.DescribeResult{Protocol: authenticationv1.Protocol, Methods: []authenticationv1.MethodDescriptor{{MethodID: "corporate-sso", ProviderID: "oidc", Kind: authenticationv1.MethodRedirect, Interaction: authenticationv1.InteractionRedirect, DisplayName: authenticationv1.LocalizedText{"en-US": "Corporate SSO"}, AMR: []string{"sso"}, ACR: "aal2"}}}), nil
	case authenticationv1.OperationBegin:
		step := authenticationv1.AuthenticationStep{StepID: strings.Repeat("s", 32), Kind: authenticationv1.StepRedirect, Title: authenticationv1.LocalizedText{"en-US": "SSO"}, Description: authenticationv1.LocalizedText{"en-US": "Continue"}, SubmitLabel: authenticationv1.LocalizedText{"en-US": "Continue"}, Fields: []authenticationv1.AuthenticationField{}, RedirectURI: "https://identity.example.test/authorize", ExpiresAt: h.now.Add(time.Minute)}
		return okResult(), mustJSON(authenticationv1.BeginResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateChallenge, Step: &step}}), nil
	case authenticationv1.OperationCancel:
		return okResult(), []byte(`{"cancelled":true}`), nil
	case authenticationv1.OperationContinue:
		var request authenticationv1.ContinueRequest
		_ = json.Unmarshal(payload, &request)
		evidence := authenticationv1.AuthenticationEvidence{EvidenceID: "evidence.1", TransactionID: request.TransactionID, MethodID: "corporate-sso", ProviderID: "oidc", Subject: authenticationv1.SubjectIdentity{ID: "alice", Issuer: "https://identity.example.test"}, AMR: []string{"oidc"}, ACR: "aal2", AuthenticatedAt: h.now, ExpiresAt: h.now.Add(30 * time.Second), Nonce: strings.Repeat("n", 32)}
		return okResult(), mustJSON(authenticationv1.ContinueResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateAuthenticated, Evidence: &evidence}}), nil
	case authenticationv1.OperationHealth:
		return okResult(), []byte(`{"ready":true,"providerId":"oidc"}`), nil
	}
	return nil, nil, nil
}

func TestBrokerSignsProfileBoundAssertionAfterRealEvidence(t *testing.T) {
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	transactions := NewMemoryTransactionStore(8)
	transactions.now = func() time.Time { return now }
	signer, _ := GenerateAssertionKey("test-key")
	service, _ := New(staticCatalog{testCatalog()}, transactions, signer)
	service.now = func() time.Time { return now }
	host := &fakeHost{now: now}
	begin := authenticationv1.BeginRequest{TransactionID: strings.Repeat("z", 32), MethodID: "corporate-sso", Audience: "authentication-provider-test", TenantID: "acme", PortalID: "management", Locale: "en-US", ClientContextDigest: strings.Repeat("c", 64)}
	_, beginRaw, _ := service.Handle(context.Background(), host, &contractv1.CallContext{}, authenticationv1.OperationBegin, mustJSON(begin))
	parsed, _ := authenticationv1.ParseMethodResult(authenticationv1.OperationBegin, beginRaw)
	step := parsed.(*authenticationv1.BeginResult).Result.Step
	request := authenticationv1.ContinueRequest{TransactionID: begin.TransactionID, StepID: step.StepID, Redirect: &authenticationv1.RedirectResponse{Code: "code", State: strings.Repeat("q", 32)}}
	result, raw, err := service.Handle(context.Background(), host, &contractv1.CallContext{}, authenticationv1.OperationContinue, mustJSON(request))
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("Broker continue 失败: %+v %v", result, err)
	}
	brokerResult, err := authenticationv1.ParseBrokerContinueResult(raw)
	if err != nil || brokerResult.Assertion == nil {
		t.Fatalf("Broker Assertion 无效: %s %v", raw, err)
	}
	if err := signer.Verify(*brokerResult.Assertion); err != nil {
		t.Fatal(err)
	}
	if brokerResult.Assertion.Payload.ProviderProfileID != "corp" || brokerResult.Assertion.Payload.Audience != "authentication-provider-test" {
		t.Fatalf("Assertion 未绑定服务端 route: %+v", brokerResult.Assertion.Payload)
	}
	trustedPortal := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "portal-host"}, Scene: "portal.bff"}
	consume := authenticationv1.ConsumeAssertionRequest{Assertion: *brokerResult.Assertion, Audience: begin.Audience, TenantID: begin.TenantID, PortalID: begin.PortalID, TransactionID: begin.TransactionID}
	consumed, consumedRaw, _ := service.Handle(context.Background(), host, trustedPortal, OperationConsumeAssertion, mustJSON(consume))
	if consumed.GetStatus() != contractv1.CallResult_STATUS_OK || string(consumedRaw) != `{"consumed":true}` {
		t.Fatalf("可信 Portal 应能消费 Assertion: %+v %s", consumed, consumedRaw)
	}
	replay, _, _ := service.Handle(context.Background(), host, trustedPortal, OperationConsumeAssertion, mustJSON(consume))
	if replay.GetStatus() != contractv1.CallResult_STATUS_ERROR || replay.Error.Code != "foundation.authentication.assertion_replayed" {
		t.Fatalf("Assertion 重放必须拒绝: %+v", replay)
	}
}

func TestBrokerRejectsAssertionConsumptionOutsideTrustedPortal(t *testing.T) {
	service, _ := New(staticCatalog{testCatalog()}, NewMemoryTransactionStore(8))
	result, _, _ := service.Handle(context.Background(), &fakeHost{}, &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER}}, OperationConsumeAssertion, []byte(`{}`))
	if result.GetStatus() != contractv1.CallResult_STATUS_ERROR || result.Error.Code != "foundation.authentication.consume_forbidden" {
		t.Fatalf("普通用户不得消费 Assertion: %+v", result)
	}
}

func testCatalog() authenticationv1.AuthenticationProviderCatalog {
	profile := compositioncommonv1.Ref{ID: "corp", Revision: 1, Digest: strings.Repeat("a", 64)}
	return authenticationv1.AuthenticationProviderCatalog{
		Document:  compositioncommonv1.Document{Version: 1, Revision: 1, ID: "catalog"},
		Providers: []authenticationv1.ProviderCatalogEntry{{Profile: profile, ContributionID: "oidc", Purposes: []authenticationv1.ProviderPurpose{authenticationv1.PurposePortalLogin}, Methods: []string{"corporate-sso"}, SubjectNamespace: "enterprise.identity.oidc", RequiredCapabilities: []string{}}},
		Bindings:  []authenticationv1.ProviderBinding{{TenantID: "acme", PortalID: "management", DefaultProvider: "corp", AllowedProviders: []string{"corp"}}},
	}
}

func TestBrokerLocksTransactionToCatalogProvider(t *testing.T) {
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	transactions := NewMemoryTransactionStore(8)
	transactions.now = func() time.Time { return now }
	service, _ := New(staticCatalog{testCatalog()}, transactions)
	service.now = func() time.Time { return now }
	host := &fakeHost{now: now}
	begin := authenticationv1.BeginRequest{TransactionID: strings.Repeat("t", 32), MethodID: "corporate-sso", Audience: "portal.example.test", TenantID: "acme", PortalID: "management", Locale: "en-US", ClientContextDigest: strings.Repeat("b", 64)}
	result, response, err := service.Handle(context.Background(), host, &contractv1.CallContext{}, authenticationv1.OperationBegin, mustJSON(begin))
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("Broker begin 失败: %+v %v", result, err)
	}
	if _, err := authenticationv1.ParseMethodResult(authenticationv1.OperationBegin, response); err != nil {
		t.Fatal(err)
	}
	if route, ok := transactions.Get(begin.TransactionID); !ok || route.ProviderID != "oidc" {
		t.Fatalf("事务未锁定 Provider: %+v", route)
	}
	result, _, _ = service.Handle(context.Background(), host, &contractv1.CallContext{}, authenticationv1.OperationCancel, mustJSON(authenticationv1.CancelRequest{TransactionID: begin.TransactionID}))
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatal("取消事务失败")
	}
	if _, ok := transactions.Get(begin.TransactionID); ok {
		t.Fatal("取消后必须删除 Broker 路由")
	}
}

func TestBrokerDescribeOnlyUsesBoundProviders(t *testing.T) {
	service, _ := New(staticCatalog{testCatalog()}, NewMemoryTransactionStore(8))
	host := &fakeHost{now: time.Now()}
	result, raw, _ := service.Handle(context.Background(), host, &contractv1.CallContext{}, authenticationv1.OperationDescribe, []byte(`{"tenantId":"acme","portalId":"management"}`))
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("describe 失败: %+v", result)
	}
	parsed, err := authenticationv1.ParseMethodResult(authenticationv1.OperationDescribe, raw)
	if err != nil || len(parsed.(*authenticationv1.DescribeResult).Methods) != 1 {
		t.Fatalf("describe 输出无效: %s %v", raw, err)
	}
}

type degradedProviderHost struct{}

func (degradedProviderHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
	if target.Capability == "database-user" {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "database.runtime.connection_unavailable"}}, nil, nil
	}
	descriptor := authenticationv1.MethodDescriptor{MethodID: "corporate-sso", ProviderID: "oidc", Kind: authenticationv1.MethodRedirect, Interaction: authenticationv1.InteractionRedirect, DisplayName: authenticationv1.LocalizedText{"en-US": "Corporate SSO"}, AMR: []string{"sso"}, ACR: "aal2"}
	return okResult(), mustJSON(authenticationv1.DescribeResult{Protocol: authenticationv1.Protocol, Methods: []authenticationv1.MethodDescriptor{descriptor}}), nil
}

func TestBrokerKeepsIndependentOIDCAvailableWhenDatabaseProviderFails(t *testing.T) {
	catalog := testCatalog()
	catalog.Providers = append(catalog.Providers, authenticationv1.ProviderCatalogEntry{Profile: compositioncommonv1.Ref{ID: "database-users", Revision: 1, Digest: strings.Repeat("d", 64)}, ContributionID: "database-user", Purposes: []authenticationv1.ProviderPurpose{authenticationv1.PurposePortalLogin}, Methods: []string{"database-password"}, SubjectNamespace: "enterprise.identity.database", RequiredCapabilities: []string{"foundation.data.relational.runtime"}})
	catalog.Bindings[0].AllowedProviders = append(catalog.Bindings[0].AllowedProviders, "database-users")
	service, _ := New(staticCatalog{catalog}, NewMemoryTransactionStore(8))
	result, raw, _ := service.Handle(context.Background(), degradedProviderHost{}, &contractv1.CallContext{}, authenticationv1.OperationDescribe, []byte(`{"tenantId":"acme","portalId":"management"}`))
	parsed, err := authenticationv1.ParseMethodResult(authenticationv1.OperationDescribe, raw)
	if result.GetStatus() != contractv1.CallResult_STATUS_OK || err != nil || len(parsed.(*authenticationv1.DescribeResult).Methods) != 1 || parsed.(*authenticationv1.DescribeResult).Methods[0].MethodID != "corporate-sso" {
		t.Fatalf("数据库 Provider 故障不应拖垮独立 OIDC: result=%+v raw=%s err=%v", result, raw, err)
	}
}

func TestBrokerStartsIsolatedTestForValidatedUnpublishedProfile(t *testing.T) {
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	transactions := NewMemoryTransactionStore(8)
	transactions.now = func() time.Time { return now }
	service, _ := New(testableCatalog{staticCatalog{testCatalog()}}, transactions)
	service.now = func() time.Time { return now }
	request := BeginProviderTestRequest{TransactionID: strings.Repeat("p", 32), ProviderProfileID: "corp-draft", MethodID: "corporate-sso", TenantID: "acme", PortalID: "management", Locale: "zh-CN", ClientContextDigest: strings.Repeat("c", 64)}
	ctx := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "portal-host"}, Scene: "portal.bff"}
	result, raw, _ := service.Handle(context.Background(), &fakeHost{now: now}, ctx, OperationBeginProviderTest, mustJSON(request))
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("未发布 Provider 测试启动失败: %+v", result)
	}
	if _, err := authenticationv1.ParseMethodResult(authenticationv1.OperationBegin, raw); err != nil {
		t.Fatal(err)
	}
	route, found := transactions.Get(request.TransactionID)
	if !found || route.ProfileID != "corp-draft" || route.Audience != "authentication-provider-test" {
		t.Fatalf("测试事务未隔离: %+v", route)
	}
}

func TestBrokerDescriptorMatchesManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	items, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil || len(items) != 2 {
		t.Fatalf("Manifest 无效: %+v %v", items, err)
	}
	runtimeDescriptors := map[string][]byte{Capability: Descriptor(), ManagementCapability: ManagementDescriptor()}
	for _, item := range items {
		var signed, runtime any
		_ = json.Unmarshal(item.Descriptor, &signed)
		_ = json.Unmarshal(runtimeDescriptors[item.ID], &runtime)
		if string(mustJSON(signed)) != string(mustJSON(runtime)) {
			t.Fatalf("descriptor 漂移: %s != %s", mustJSON(signed), mustJSON(runtime))
		}
	}
}

func mustJSON(value any) []byte { raw, _ := json.Marshal(value); return raw }
