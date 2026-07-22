package authenticationv1_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

func validAccessProfile(id, route string) authenticationv1.AccessProfile {
	return authenticationv1.AccessProfile{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: id},
		TenantID: "acme", PortalID: "operations", Route: route, Domains: []string{"portal.example.test"},
		PlatformProfile: compositioncommonv1.Ref{ID: "portal-default", Revision: 3, Digest: strings.Repeat("a", 64)},
		AccessTemplate:  "access", Localization: authenticationv1.AccessLocalizationPolicy{DefaultLocale: "zh-CN", SupportedLocales: []string{"zh-CN", "en-US"}},
		Authentication: authenticationv1.AccessMethodPolicy{AllowedMethods: []string{"password", "one-time-code"}, DefaultMethod: "password", ReuseIdentifier: true},
		Branding:       authenticationv1.AccessBranding{ProductName: localized("VastPlan"), SupportPath: "/support", PrivacyPath: "/privacy"},
	}
}

func TestAccessBootstrapExcludesServerSideSelectionData(t *testing.T) {
	bootstrap := authenticationv1.AccessBootstrap{
		SchemaVersion: authenticationv1.SchemaVersion, GenerationID: strings.Repeat("b", 64), AccessTemplate: "access",
		Localization:   authenticationv1.AccessLocalizationPolicy{DefaultLocale: "zh-CN", SupportedLocales: []string{"zh-CN", "en-US"}},
		Authentication: authenticationv1.AccessMethodPolicy{AllowedMethods: []string{"password"}, DefaultMethod: "password", ReuseIdentifier: true},
		Branding:       authenticationv1.PublicAccessBranding{ProductName: localized("VastPlan")},
	}
	parsed, err := authenticationv1.ParseAccessBootstrap(marshal(t, bootstrap))
	if err != nil || parsed.GenerationID != bootstrap.GenerationID {
		t.Fatalf("Access Bootstrap 解析失败: %+v err=%v", parsed, err)
	}
	var public map[string]any
	if err := json.Unmarshal(marshal(t, bootstrap), &public); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"tenantId", "portalId", "platformProfile"} {
		if _, exists := public[forbidden]; exists {
			t.Fatalf("公共 bootstrap 不得暴露 %s", forbidden)
		}
	}
	public["branding"].(map[string]any)["logoSha256"] = strings.Repeat("c", 64)
	if _, err := authenticationv1.ParseAccessBootstrap(marshal(t, public)); err == nil {
		t.Fatal("公共 bootstrap 不得接受内部 Logo 摘要")
	}
}

func TestAccessProfileReferencesExistingFrontendFoundationProfile(t *testing.T) {
	profile, err := authenticationv1.ParseAccessProfile(marshal(t, validAccessProfile("operations-login", "/")))
	if err != nil {
		t.Fatal(err)
	}
	if profile.PlatformProfile.ID != "portal-default" || profile.AccessTemplate != "access" || len(profile.Digest()) != 64 {
		t.Fatalf("Access Profile 解析异常: %+v", profile)
	}
	invalid := validAccessProfile("invalid", "/")
	invalid.Authentication.DefaultMethod = "passkey"
	if _, err := authenticationv1.ParseAccessProfile(marshal(t, invalid)); err == nil {
		t.Fatal("默认认证方式必须受 Access Profile 允许目录约束")
	}
}

func TestAccessCatalogUsesHostAndLongestRouteWithoutPrincipal(t *testing.T) {
	root := validAccessProfile("root-login", "/")
	admin := validAccessProfile("admin-login", "/admin")
	admin.PortalID = "admin"
	catalog := authenticationv1.AccessProfileCatalog{Document: compositioncommonv1.Document{Version: 1, Revision: 4, ID: "access"}, Profiles: []authenticationv1.AccessProfile{root, admin}}
	parsed, err := authenticationv1.ParseAccessProfileCatalog(marshal(t, catalog))
	if err != nil {
		t.Fatal(err)
	}
	profile, found := parsed.Resolve("PORTAL.EXAMPLE.TEST.", "/admin/settings")
	if !found || profile.ID != "admin-login" {
		t.Fatalf("应按域名和最长 route 解析会话前 Profile: %+v found=%v", profile, found)
	}
	if _, found := parsed.Resolve("attacker.example.test", "/"); found {
		t.Fatal("未知域名不得回退到默认登录页")
	}
}

func TestAccessCatalogRejectsAmbiguousPublicRoute(t *testing.T) {
	first := validAccessProfile("first", "/")
	second := validAccessProfile("second", "/")
	catalog := authenticationv1.AccessProfileCatalog{Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "access"}, Profiles: []authenticationv1.AccessProfile{first, second}}
	if _, err := authenticationv1.ParseAccessProfileCatalog(marshal(t, catalog)); err == nil {
		t.Fatal("同域同 route 不能选择两个 Access Profile")
	}
}

func TestAuthenticationAssertionIsOneUseSizedAndContainsNoAuthorization(t *testing.T) {
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	payload := authenticationv1.AuthenticationAssertion{
		SchemaVersion: authenticationv1.SchemaVersion, AssertionID: "assertion.00000001", TransactionID: strings.Repeat("t", 32),
		ProviderID: "enterprise-oidc", ProviderProfileID: "corporate-oidc",
		Subject: authenticationv1.SubjectIdentity{ID: "alice", Issuer: "https://identity.example.test"}, TenantID: "acme", PortalID: "operations", Audience: "portal.example.test",
		AMR: []string{"pwd"}, ACR: "aal1", IssuedAt: now, ExpiresAt: now.Add(30 * time.Second), Nonce: strings.Repeat("n", 32),
	}
	signed := authenticationv1.SignedAuthenticationAssertion{Payload: payload, Signature: authenticationv1.Signature{Algorithm: "Ed25519", KeyID: "auth-key.1", Value: strings.Repeat("A", 86)}}
	if _, err := authenticationv1.ParseSignedAssertion(marshal(t, signed)); err != nil {
		t.Fatal(err)
	}
	left, _ := authenticationv1.AssertionDigest(payload)
	payload.AMR = []string{"mfa", "pwd"}
	rightA, _ := authenticationv1.AssertionDigest(payload)
	payload.AMR = []string{"pwd", "mfa"}
	rightB, _ := authenticationv1.AssertionDigest(payload)
	if left == rightA || rightA != rightB {
		t.Fatal("Assertion 摘要必须绑定 AMR 且与集合顺序无关")
	}
	raw := marshal(t, signed)
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	payloadObject := object["payload"].(map[string]any)
	payloadObject["roles"] = []string{"platform.admin"}
	if _, err := authenticationv1.ParseSignedAssertion(marshal(t, object)); err == nil {
		t.Fatal("Authentication Assertion 不得携带授权角色")
	}
}

func TestBrokerContinueResultRequiresAssertionOnlyAfterAuthentication(t *testing.T) {
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	evidence := authenticationv1.AuthenticationEvidence{
		EvidenceID: "evidence.00000001", TransactionID: strings.Repeat("t", 32), MethodID: "password", ProviderID: "enterprise-oidc",
		Subject: authenticationv1.SubjectIdentity{ID: "alice", Issuer: "https://identity.example.test"},
		AMR:     []string{"pwd"}, ACR: "aal1", AuthenticatedAt: now, ExpiresAt: now.Add(30 * time.Second), Nonce: strings.Repeat("n", 32),
	}
	payload := authenticationv1.AuthenticationAssertion{
		SchemaVersion: authenticationv1.SchemaVersion, AssertionID: "assertion.00000001", TransactionID: evidence.TransactionID,
		ProviderID: evidence.ProviderID, ProviderProfileID: "corporate-oidc", Subject: evidence.Subject,
		TenantID: "acme", PortalID: "operations", Audience: "portal.example.test", AMR: evidence.AMR, ACR: evidence.ACR,
		IssuedAt: now, ExpiresAt: now.Add(30 * time.Second), Nonce: evidence.Nonce,
	}
	signed := authenticationv1.SignedAuthenticationAssertion{Payload: payload, Signature: authenticationv1.Signature{Algorithm: "Ed25519", KeyID: "auth-key.1", Value: strings.Repeat("A", 86)}}
	authenticated := authenticationv1.BrokerContinueResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateAuthenticated, Evidence: &evidence}, Assertion: &signed}
	if _, err := authenticationv1.ParseBrokerContinueResult(marshal(t, authenticated)); err != nil {
		t.Fatal(err)
	}
	authenticated.Assertion = nil
	if _, err := authenticationv1.ParseBrokerContinueResult(marshal(t, authenticated)); err == nil {
		t.Fatal("authenticated Broker 响应必须携带 Assertion")
	}
	step := passwordStep(now)
	challenge := authenticationv1.BrokerContinueResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateChallenge, Step: &step}}
	challenge.Assertion = &signed
	if _, err := authenticationv1.ParseBrokerContinueResult(marshal(t, challenge)); err == nil {
		t.Fatal("非 authenticated Broker 响应不得携带 Assertion")
	}
}
