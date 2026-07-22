package authorizationv1_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
)

func validPolicy() authorizationv1.AuthorizationIR {
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	config := authorizationv1.ConfigurationRevisionRef{ProfileID: "authorization.default", Revision: 1, Digest: strings.Repeat("c", 64)}
	profile := authorizationv1.ProviderProfile{
		ID: "authorization.default", Revision: 1,
		Store:    authorizationv1.ProviderRef{Protocol: authorizationv1.ProtocolStore, ProviderID: "native-store", PluginID: "cn.vastplan.foundation.security.authorization-store", Capability: "foundation.security.authorization.store", Version: "1.0.0", Configuration: config},
		Engine:   authorizationv1.ProviderRef{Protocol: authorizationv1.ProtocolEngine, ProviderID: "native-rbac", PluginID: "cn.vastplan.foundation.security.authorization-engine", Capability: "foundation.security.authorization.engine", Version: "1.0.0", Configuration: config},
		Exchange: []authorizationv1.ProviderRef{},
	}
	permissions := []string{"tenant.orders.write", "tenant.orders.read"}
	return authorizationv1.AuthorizationIR{
		SchemaVersion: authorizationv1.IRSchemaVersion, CatalogDigest: strings.Repeat("a", 64), RootDomainID: "platform.root",
		ProviderProfiles: []authorizationv1.ProviderProfile{profile},
		Domains: []authorizationv1.PolicyDomain{
			{ID: "tenant.acme", Revision: 1, Kind: authorizationv1.DomainTenant, ParentID: "platform.root", Scope: authorizationv1.DomainScope{TenantID: "acme"}, ProviderProfileID: profile.ID, Delegation: authorizationv1.DelegationCeiling{Permissions: []string{"tenant.orders.read"}, MaxRisk: authorizationv1.RiskMedium, MayDelegate: false, OfflineAllowed: false, MaxTTLSeconds: 600}},
			{ID: "platform.root", Revision: 1, Kind: authorizationv1.DomainPlatform, Scope: authorizationv1.DomainScope{}, ProviderProfileID: profile.ID, Delegation: authorizationv1.DelegationCeiling{Permissions: permissions, MaxRisk: authorizationv1.RiskCritical, MayDelegate: true, OfflineAllowed: false, MaxTTLSeconds: 3600}},
		},
		Roles:       []authorizationv1.CompiledRole{{ID: "orders.viewer", Revision: 2, DomainID: "tenant.acme", Statements: []authorizationv1.PolicyStatement{{ID: "read-orders", Effect: authorizationv1.EffectAllow, Permissions: []string{"tenant.orders.read"}, Constraints: []authorizationv1.AttributeConstraint{}}}}},
		Bindings:    []authorizationv1.SubjectBinding{{ID: "binding.alice.viewer", Revision: 3, DomainID: "tenant.acme", Subject: authorizationv1.Subject{Kind: authorizationv1.SubjectUser, ID: "alice", Issuer: "https://idp.example.test"}, RoleID: "orders.viewer", RoleRevision: 2, NotBefore: now, ExpiresAt: now.Add(time.Hour)}},
		Revocations: []authorizationv1.Revocation{}, RevocationRevision: 0,
	}
}

func TestAuthorizationIRRoundTripsStrictSchemaAndSemanticValidation(t *testing.T) {
	policy := validPolicy()
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := authorizationv1.ParseAuthorizationIR(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.RootDomainID != "platform.root" || len(parsed.Domains) != 2 {
		t.Fatalf("解析结果异常: %+v", parsed)
	}
	withUnknown := raw[:len(raw)-1]
	withUnknown = append(withUnknown, []byte(`,"providerPolicy":"allow-all"}`)...)
	if _, err := authorizationv1.ParseAuthorizationIR(withUnknown); err == nil {
		t.Fatal("Authorization IR 未知字段必须被拒绝")
	}
}

func TestAuthorizationIRDigestIsIndependentOfSetOrdering(t *testing.T) {
	first := validPolicy()
	second := validPolicy()
	second.Domains[0], second.Domains[1] = second.Domains[1], second.Domains[0]
	second.Domains[0].Delegation.Permissions[0], second.Domains[0].Delegation.Permissions[1] = second.Domains[0].Delegation.Permissions[1], second.Domains[0].Delegation.Permissions[0]
	left, err := authorizationv1.AuthorizationIRDigest(first)
	if err != nil {
		t.Fatal(err)
	}
	right, err := authorizationv1.AuthorizationIRDigest(second)
	if err != nil {
		t.Fatal(err)
	}
	if left != right {
		t.Fatalf("规范化 IR 摘要不稳定: %s != %s", left, right)
	}
}

func TestPolicyDomainCannotWidenParentDelegation(t *testing.T) {
	policy := validPolicy()
	policy.Domains[0].Delegation.Permissions = append(policy.Domains[0].Delegation.Permissions, "tenant.orders.write")
	policy.Domains[0].Delegation.MaxRisk = authorizationv1.RiskCritical
	policy.Domains[1].Delegation.MaxRisk = authorizationv1.RiskMedium
	if err := authorizationv1.ValidateAuthorizationIR(policy); err == nil || !strings.Contains(err.Error(), "风险") {
		t.Fatalf("子 Domain 扩大风险上限必须拒绝: %v", err)
	}
	policy = validPolicy()
	policy.Domains[0].Delegation.Permissions = append(policy.Domains[0].Delegation.Permissions, "tenant.billing.write")
	if err := authorizationv1.ValidateAuthorizationIR(policy); err == nil || !strings.Contains(err.Error(), "超出父级") {
		t.Fatalf("子 Domain 扩大权限必须拒绝: %v", err)
	}
}

func TestRoleAndBindingCannotEscapePolicyDomain(t *testing.T) {
	policy := validPolicy()
	policy.Roles[0].Statements[0].Permissions = []string{"tenant.orders.write"}
	if err := authorizationv1.ValidateAuthorizationIR(policy); err == nil || !strings.Contains(err.Error(), "委托上限") {
		t.Fatalf("Role 超权必须拒绝: %v", err)
	}
	policy = validPolicy()
	policy.Bindings[0].DomainID = "platform.root"
	if err := authorizationv1.ValidateAuthorizationIR(policy); err == nil || !strings.Contains(err.Error(), "同 Domain") {
		t.Fatalf("Binding 跨 Domain 引用必须拒绝: %v", err)
	}
}

func TestSignedPolicySnapshotWireShapeIsStrictButDoesNotPretendToVerifyTrust(t *testing.T) {
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	signed := authorizationv1.SignedPolicySnapshot{
		Payload: authorizationv1.PolicySnapshot{
			SchemaVersion: authorizationv1.IRSchemaVersion, SnapshotID: "snapshot.0000000001", Revision: 1,
			Audience: []string{"platform.acme"}, IssuedAt: now, NotBefore: now, ExpiresAt: now.Add(5 * time.Minute), Policy: validPolicy(),
		},
		Signature: authorizationv1.Signature{Algorithm: "Ed25519", KeyID: "policy-key.1", Value: strings.Repeat("A", 86)},
	}
	raw, err := json.Marshal(signed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authorizationv1.ParseSignedPolicySnapshot(raw); err != nil {
		t.Fatal(err)
	}
	if _, err := authorizationv1.ParseProviderRequest(authorizationv1.ProtocolEngine, "prepare", marshal(t, authorizationv1.EnginePrepareRequest{Snapshot: signed})); err != nil {
		t.Fatal(err)
	}
	signed.Payload.ExpiresAt = signed.Payload.NotBefore
	if err := authorizationv1.ValidatePolicySnapshot(signed.Payload); err == nil {
		t.Fatal("无效 Snapshot 时间窗必须拒绝")
	}
}
