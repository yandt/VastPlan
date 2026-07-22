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

func (s staticCatalog) Load() (authenticationv1.AuthenticationProviderCatalog, error) {
	return s.value, nil
}

type fakeHost struct {
	calls []string
	now   time.Time
}

func (h *fakeHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
	h.calls = append(h.calls, target.Capability+"/"+target.GetOperation())
	switch target.GetOperation() {
	case authenticationv1.OperationDescribe:
		return okResult(), mustJSON(authenticationv1.DescribeResult{Protocol: authenticationv1.Protocol, Methods: []authenticationv1.MethodDescriptor{{MethodID: "corporate-sso", ProviderID: "oidc", Kind: authenticationv1.MethodRedirect, Interaction: authenticationv1.InteractionRedirect, DisplayName: authenticationv1.LocalizedText{"en-US": "Corporate SSO"}, AMR: []string{"sso"}, ACR: "aal2"}}}), nil
	case authenticationv1.OperationBegin:
		step := authenticationv1.AuthenticationStep{StepID: strings.Repeat("s", 32), Kind: authenticationv1.StepRedirect, Title: authenticationv1.LocalizedText{"en-US": "SSO"}, Description: authenticationv1.LocalizedText{"en-US": "Continue"}, SubmitLabel: authenticationv1.LocalizedText{"en-US": "Continue"}, Fields: []authenticationv1.AuthenticationField{}, RedirectURI: "https://identity.example.test/authorize", ExpiresAt: h.now.Add(time.Minute)}
		return okResult(), mustJSON(authenticationv1.BeginResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateChallenge, Step: &step}}), nil
	case authenticationv1.OperationCancel:
		return okResult(), []byte(`{"cancelled":true}`), nil
	case authenticationv1.OperationHealth:
		return okResult(), []byte(`{"ready":true,"providerId":"oidc"}`), nil
	}
	return nil, nil, nil
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
	if err != nil || len(items) != 1 {
		t.Fatalf("Manifest 无效: %+v %v", items, err)
	}
	var signed, runtime any
	_ = json.Unmarshal(items[0].Descriptor, &signed)
	_ = json.Unmarshal(Descriptor(), &runtime)
	if string(mustJSON(signed)) != string(mustJSON(runtime)) {
		t.Fatalf("descriptor 漂移: %s != %s", mustJSON(signed), mustJSON(runtime))
	}
}

func mustJSON(value any) []byte { raw, _ := json.Marshal(value); return raw }
