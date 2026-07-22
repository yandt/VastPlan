package broker

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

func managedProfile() authenticationv1.AuthenticationProviderProfile {
	return authenticationv1.AuthenticationProviderProfile{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "corporate-oidc"}, ContributionID: "enterprise-oidc",
		Configuration: compositioncommonv1.Ref{ID: "corporate-oidc-config", Revision: 1, Digest: strings.Repeat("a", 64)},
		Purposes:      []authenticationv1.ProviderPurpose{authenticationv1.PurposePortalLogin}, Methods: []string{"oidc"}, SubjectNamespace: "enterprise.identity.oidc", RequiredCapabilities: []string{},
	}
}

func TestManagementPublishesOnlyTestedAndSeparatelyApprovedProvider(t *testing.T) {
	store := &FileManagementStore{Path: filepath.Join(t.TempDir(), "providers.json")}
	assertions, _ := GenerateAssertionKey("test-key")
	service, _ := NewManagementService(store, assertions)
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	state, err := service.createDraft(CreateDraftRequest{ExpectedGeneration: 0, Profile: managedProfile()})
	if err != nil || state.Generation != 1 {
		t.Fatalf("创建 Draft 失败: %+v %v", state, err)
	}
	state, err = service.transition(ProviderActionRequest{ExpectedGeneration: state.Generation, ProviderID: "corporate-oidc"}, authenticationv1.ProviderValidated)
	if err != nil {
		t.Fatal(err)
	}
	testAssertion, _ := assertions.Sign(authenticationv1.AuthenticationAssertion{SchemaVersion: authenticationv1.SchemaVersion, AssertionID: "assertion.test", TransactionID: strings.Repeat("t", 32), ProviderID: "enterprise-oidc", ProviderProfileID: "corporate-oidc", Subject: authenticationv1.SubjectIdentity{ID: "alice", Issuer: "https://identity.example"}, TenantID: "acme", PortalID: "management", Audience: "authentication-provider-test", AMR: []string{"oidc"}, ACR: "aal1", IssuedAt: now, ExpiresAt: now.Add(30 * time.Second), Nonce: strings.Repeat("n", 32)})
	state, err = service.recordTest(RecordTestRequest{ExpectedGeneration: state.Generation, ProviderID: "corporate-oidc", Actor: "tester", Assertion: testAssertion})
	if err != nil || state.Providers[0].Lifecycle.State != authenticationv1.ProviderTested {
		t.Fatalf("测试失败: %+v %v", state, err)
	}
	if _, err = service.approve(ApproveRequest{ExpectedGeneration: state.Generation, ProviderID: "corporate-oidc", Actor: "tester"}); err == nil {
		t.Fatal("测试人不得批准自己的结果")
	}
	state, err = service.approve(ApproveRequest{ExpectedGeneration: state.Generation, ProviderID: "corporate-oidc", Actor: "approver"})
	if err != nil {
		t.Fatal(err)
	}
	access := authenticationv1.AccessProfileCatalog{Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "access"}, Profiles: []authenticationv1.AccessProfile{{Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "management-login"}, TenantID: "acme", PortalID: "management", Route: "/", Domains: []string{"portal.example.test"}, PlatformProfile: compositioncommonv1.Ref{ID: "portal", Revision: 1, Digest: strings.Repeat("d", 64)}, AccessTemplate: "access", Localization: authenticationv1.AccessLocalizationPolicy{DefaultLocale: "zh-CN", SupportedLocales: []string{"zh-CN"}}, Authentication: authenticationv1.AccessMethodPolicy{AllowedMethods: []string{"oidc"}, DefaultMethod: "oidc", ReuseIdentifier: false}, Branding: authenticationv1.AccessBranding{ProductName: authenticationv1.LocalizedText{"zh-CN": "VastPlan"}}}}}
	state, err = service.publish(PublishRequest{ExpectedGeneration: state.Generation, CatalogID: "enterprise-auth", CatalogRevision: 1, Bindings: []authenticationv1.ProviderBinding{{TenantID: "acme", PortalID: "management", DefaultProvider: "corporate-oidc", AllowedProviders: []string{"corporate-oidc"}}}, AccessCatalog: access})
	if err != nil {
		t.Fatal(err)
	}
	if state.Catalog == nil || state.AccessCatalog == nil || state.Providers[0].Lifecycle.State != authenticationv1.ProviderPublished {
		t.Fatalf("发布未原子推进 Catalog/Lifecycle: %+v", state)
	}
	resolved, found := state.Catalog.Resolve("acme", "management", "oidc")
	if !found || resolved.ContributionID != "enterprise-oidc" {
		t.Fatalf("已发布 Catalog 不可路由: %+v", resolved)
	}
}

func TestManagementKeepsBlockedDatabaseProviderOutOfCatalog(t *testing.T) {
	store := &FileManagementStore{Path: filepath.Join(t.TempDir(), "providers.json")}
	service, _ := NewManagementService(store)
	profile := managedProfile()
	profile.ID = "database-users"
	profile.ContributionID = "database-user"
	profile.Configuration.ID = "database-users-config"
	profile.Methods = []string{"database-password"}
	profile.SubjectNamespace = "enterprise.identity.database"
	profile.RequiredCapabilities = []string{"database.provider"}
	state, _ := service.createDraft(CreateDraftRequest{Profile: profile})
	state, _ = service.transition(ProviderActionRequest{ExpectedGeneration: state.Generation, ProviderID: profile.ID}, authenticationv1.ProviderValidated)
	state, err := service.reconcileReadiness(state.Generation, profile.ID, []string{"database.provider"})
	if err != nil {
		t.Fatal(err)
	}
	if state.Providers[0].Lifecycle.Readiness != authenticationv1.ProviderBlocked {
		t.Fatal("缺少数据库能力时必须 Blocked")
	}
	if _, err := service.approve(ApproveRequest{ExpectedGeneration: state.Generation, ProviderID: profile.ID, Actor: "approver"}); err == nil {
		t.Fatal("Blocked Provider 不得批准")
	}
}
