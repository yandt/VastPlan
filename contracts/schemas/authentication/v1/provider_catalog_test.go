package authenticationv1_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

func providerProfile(id, contribution, method string, capabilities ...string) authenticationv1.AuthenticationProviderProfile {
	if capabilities == nil {
		capabilities = []string{}
	}
	return authenticationv1.AuthenticationProviderProfile{
		Document:       compositioncommonv1.Document{Version: 1, Revision: 1, ID: id},
		ContributionID: contribution,
		Configuration:  compositioncommonv1.Ref{ID: id + "-configuration", Revision: 1, Digest: strings.Repeat("a", 64)},
		Purposes:       []authenticationv1.ProviderPurpose{authenticationv1.PurposePortalLogin},
		Methods:        []string{method}, SubjectNamespace: "enterprise.identity." + id,
		RequiredCapabilities: capabilities,
	}
}

func providerEntry(profile authenticationv1.AuthenticationProviderProfile) authenticationv1.ProviderCatalogEntry {
	return authenticationv1.ProviderCatalogEntry{
		Profile:        compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()},
		ContributionID: profile.ContributionID, Purposes: profile.Purposes, Methods: profile.Methods,
		SubjectNamespace: profile.SubjectNamespace, RequiredCapabilities: profile.RequiredCapabilities,
	}
}

func TestProviderProfileContainsNoEnterpriseUsersOrSecrets(t *testing.T) {
	profile := providerProfile("corporate-oidc", "oidc", "corporate-sso", "network.egress.identity")
	parsed, err := authenticationv1.ParseAuthenticationProviderProfile(marshal(t, profile))
	if err != nil || len(parsed.Digest()) != 64 {
		t.Fatalf("Provider Profile 解析失败: %+v err=%v", parsed, err)
	}
	object := map[string]any{}
	if err := json.Unmarshal(marshal(t, profile), &object); err != nil {
		t.Fatal(err)
	}
	object["clientSecret"] = "secret"
	if _, err := authenticationv1.ParseAuthenticationProviderProfile(marshal(t, object)); err == nil {
		t.Fatal("Provider Profile 不得保存凭证材料")
	}
	delete(object, "clientSecret")
	object["users"] = []any{map[string]any{"id": "alice"}}
	if _, err := authenticationv1.ParseAuthenticationProviderProfile(marshal(t, object)); err == nil {
		t.Fatal("平台 Provider Profile 不得成为企业用户库")
	}
}

func TestProviderCatalogRoutesMultiplePluginsWithoutAmbiguity(t *testing.T) {
	oidc := providerProfile("corporate-oidc", "oidc", "corporate-sso", "network.egress.identity")
	local := providerProfile("seed-local", "seed-local", "seed-recovery")
	catalog := authenticationv1.AuthenticationProviderCatalog{
		Document:  compositioncommonv1.Document{Version: 1, Revision: 1, ID: "enterprise-authentication"},
		Providers: []authenticationv1.ProviderCatalogEntry{providerEntry(local), providerEntry(oidc)},
		Bindings:  []authenticationv1.ProviderBinding{{TenantID: "acme", PortalID: "platform", DefaultProvider: oidc.ID, AllowedProviders: []string{oidc.ID, local.ID}}},
	}
	parsed, err := authenticationv1.ParseAuthenticationProviderCatalog(marshal(t, catalog))
	if err != nil {
		t.Fatal(err)
	}
	selected, found := parsed.Resolve("acme", "platform", "corporate-sso")
	if !found || selected.Profile.ID != oidc.ID {
		t.Fatalf("认证方式应解析到 OIDC Provider: %+v found=%v", selected, found)
	}

	local.Methods = []string{"corporate-sso"}
	catalog.Providers[0] = providerEntry(local)
	if _, err := authenticationv1.ParseAuthenticationProviderCatalog(marshal(t, catalog)); err == nil {
		t.Fatal("同一 Portal 的认证方式不得路由到多个 Provider")
	}
}

func TestDatabaseProviderMayRemainBlockedWithoutBlockingCatalogDesign(t *testing.T) {
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	profile := providerProfile("database-users", "database-user", "database-password", "database.provider")
	lifecycle := authenticationv1.AuthenticationProviderLifecycle{
		SchemaVersion: authenticationv1.SchemaVersion,
		Profile:       compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()},
		State:         authenticationv1.ProviderValidated, Readiness: authenticationv1.ProviderBlocked,
		UnmetCapabilities: []string{"database.provider"}, UpdatedAt: now,
	}
	if _, err := authenticationv1.ParseAuthenticationProviderLifecycle(marshal(t, lifecycle)); err != nil {
		t.Fatalf("未配置数据库时 Provider 应进入 Blocked 而非破坏认证契约: %v", err)
	}
	lifecycle.Readiness = authenticationv1.ProviderReady
	if _, err := authenticationv1.ParseAuthenticationProviderLifecycle(marshal(t, lifecycle)); err == nil {
		t.Fatal("仍有未满足依赖的 Provider 不得报告 Ready")
	}
}

func TestProviderLifecycleUsesOneTransitionTable(t *testing.T) {
	if !authenticationv1.CanTransitionProvider(authenticationv1.ProviderApproved, authenticationv1.ProviderPublished) {
		t.Fatal("Approved 必须可以发布")
	}
	if authenticationv1.CanTransitionProvider(authenticationv1.ProviderDraft, authenticationv1.ProviderPublished) {
		t.Fatal("Draft 不得绕过验证、测试和批准直接发布")
	}
	keyA := authenticationv1.StableSubjectKey("corp-a", "https://issuer.example", "alice")
	keyB := authenticationv1.StableSubjectKey("corp-b", "https://issuer.example", "alice")
	if keyA == keyB {
		t.Fatal("不同 Provider Profile 的外部 subject 不得碰撞")
	}
}
