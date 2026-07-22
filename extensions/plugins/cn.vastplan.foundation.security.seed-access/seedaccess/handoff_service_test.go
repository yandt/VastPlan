package seedaccess

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
)

type acceptingAssertionProof struct{}

func (acceptingAssertionProof) Verify(authenticationv1.SignedAuthenticationAssertion) error {
	return nil
}

type staticPolicyProof struct {
	value authorizationv1.SignedPolicySnapshot
}

func (s staticPolicyProof) Load() (authorizationv1.SignedPolicySnapshot, error) { return s.value, nil }

func TestHandoffServiceRequiresBrokerAssertionAndBoundPolicyBeforeAtomicDisable(t *testing.T) {
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	authority, _ := NewAuthority(FileStore{Path: filepath.Join(t.TempDir(), "seed.json")}, nil)
	authority.now = func() time.Time { return now }
	state, _ := authority.Initialize("seed-owner", []byte("correct horse battery staple"))
	profile := testRef("enterprise-users", "a")
	assertion := testHandoffAssertion(now, profile.ID)
	policy := testHandoffPolicy(now, authenticationv1.StableSubjectID(profile.ID, assertion.Payload.Subject.Issuer, assertion.Payload.Subject.ID))
	service, _ := NewHandoffService(authority, acceptingAssertionProof{}, staticPolicyProof{value: policy})
	service.now = func() time.Time { return now.Add(time.Second) }
	ctx := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "seed-owner"}, Scene: "portal.bff", TenantId: "acme"}

	state = callHandoff(t, service, ctx, "configureProvider", handoffRequest{ExpectedGeneration: state.Generation, ProviderProfile: profile})
	if state.Phase != ProviderConfigured {
		t.Fatalf("Provider 配置状态错误: %+v", state)
	}
	state = callHandoff(t, service, ctx, "verifyProvider", handoffRequest{ExpectedGeneration: state.Generation, ProviderProfile: profile, Assertion: assertion})
	if state.Phase != ProviderVerified {
		t.Fatalf("Provider 验证状态错误: %+v", state)
	}
	state = callHandoff(t, service, ctx, "prepareHandoff", handoffRequest{ExpectedGeneration: state.Generation, ProviderProfile: profile, Assertion: assertion, RecoveryReady: true})
	if state.Phase != HandoffReady || state.Handoff == nil || state.Handoff.PolicySnapshot.ID != "snapshot.platform" {
		t.Fatalf("交接准备错误: %+v", state)
	}
	state = callHandoff(t, service, ctx, "completeHandoff", handoffRequest{ExpectedGeneration: state.Generation, SealDigest: state.Handoff.Digest})
	if state.Phase != EnterpriseActive || state.Operator == nil {
		t.Fatalf("企业交接未完成: %+v", state)
	}
	if err := authority.Authenticate("seed-owner", []byte("correct horse battery staple"), nil); err == nil {
		t.Fatal("交接完成后 Seed 登录必须失效")
	}
}

func TestHandoffServiceRejectsPolicyWithoutEnterpriseSubject(t *testing.T) {
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	authority, _ := NewAuthority(FileStore{Path: filepath.Join(t.TempDir(), "seed.json")}, nil)
	authority.now = func() time.Time { return now }
	state, _ := authority.Initialize("seed-owner", []byte("correct horse battery staple"))
	profile := testRef("enterprise-users", "a")
	state, _ = authority.ConfigureProvider(state.Generation, profile)
	assertion := testHandoffAssertion(now, profile.ID)
	state, _ = authority.VerifyProvider(state.Generation, profile, assertion.Payload.Subject)
	service, _ := NewHandoffService(authority, acceptingAssertionProof{}, staticPolicyProof{value: testHandoffPolicy(now, "subject."+strings.Repeat("f", 64))})
	service.now = func() time.Time { return now.Add(time.Second) }
	result, _, _ := service.Handle(&contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER}, Scene: "portal.bff"}, "prepareHandoff", mustJSON(handoffRequest{ExpectedGeneration: state.Generation, ProviderProfile: profile, Assertion: assertion, RecoveryReady: true}))
	if result.GetStatus() != contractv1.CallResult_STATUS_ERROR {
		t.Fatal("未授权企业主体不得完成 Seed 交接")
	}
}

func callHandoff(t *testing.T, service *HandoffService, ctx *contractv1.CallContext, operation string, request handoffRequest) State {
	t.Helper()
	result, raw, err := service.Handle(ctx, operation, mustJSON(request))
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("%s 失败: %+v %v", operation, result, err)
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func testHandoffAssertion(now time.Time, profileID string) authenticationv1.SignedAuthenticationAssertion {
	return authenticationv1.SignedAuthenticationAssertion{Payload: authenticationv1.AuthenticationAssertion{SchemaVersion: "v1", AssertionID: "assertion.00000001", TransactionID: strings.Repeat("t", 32), ProviderID: "database", ProviderProfileID: profileID, Subject: authenticationv1.SubjectIdentity{ID: "alice", Issuer: "urn:users"}, TenantID: "acme", PortalID: "operations", Audience: "portal:example.test:operations", AMR: []string{"pwd"}, ACR: "aal1", IssuedAt: now, ExpiresAt: now.Add(30 * time.Second), Nonce: strings.Repeat("n", 32)}, Signature: authenticationv1.Signature{Algorithm: "Ed25519", KeyID: "broker.1", Value: strings.Repeat("A", 86)}}
}

func testHandoffPolicy(now time.Time, subjectID string) authorizationv1.SignedPolicySnapshot {
	config := authorizationv1.ConfigurationRevisionRef{ProfileID: "authorization.default", Revision: 1, Digest: strings.Repeat("c", 64)}
	provider := authorizationv1.ProviderProfile{ID: "authorization.default", Revision: 1, Store: authorizationv1.ProviderRef{Protocol: authorizationv1.ProtocolStore, ProviderID: "store", PluginID: "cn.vastplan.foundation.security.authorization-store", Capability: "foundation.security.authorization.store", Version: "1.0.0", Configuration: config}, Engine: authorizationv1.ProviderRef{Protocol: authorizationv1.ProtocolEngine, ProviderID: "engine", PluginID: "cn.vastplan.foundation.security.authorization-engine", Capability: "foundation.security.authorization.engine", Version: "1.0.0", Configuration: config}, Exchange: []authorizationv1.ProviderRef{}}
	policy := authorizationv1.AuthorizationIR{SchemaVersion: "v1", CatalogDigest: strings.Repeat("d", 64), RootDomainID: "platform.root", ProviderProfiles: []authorizationv1.ProviderProfile{provider}, Domains: []authorizationv1.PolicyDomain{{ID: "platform.root", Revision: 1, Kind: authorizationv1.DomainPlatform, ProviderProfileID: provider.ID, Delegation: authorizationv1.DelegationCeiling{Permissions: []string{"foundation.security.seed.handoff.complete"}, MaxRisk: authorizationv1.RiskCritical, MayDelegate: true, MaxTTLSeconds: 3600}}}, Roles: []authorizationv1.CompiledRole{{ID: "platform.owner", Revision: 1, DomainID: "platform.root", Statements: []authorizationv1.PolicyStatement{{ID: "handoff", Effect: authorizationv1.EffectAllow, Permissions: []string{"foundation.security.seed.handoff.complete"}, Constraints: []authorizationv1.AttributeConstraint{}}}}}, Bindings: []authorizationv1.SubjectBinding{{ID: "binding.enterprise.owner", Revision: 1, DomainID: "platform.root", Subject: authorizationv1.Subject{Kind: authorizationv1.SubjectUser, ID: subjectID, Issuer: authenticationv1.StableSubjectIssuer}, RoleID: "platform.owner", RoleRevision: 1, NotBefore: now, ExpiresAt: now.Add(time.Hour)}}, Revocations: []authorizationv1.Revocation{}}
	return authorizationv1.SignedPolicySnapshot{Payload: authorizationv1.PolicySnapshot{SchemaVersion: "v1", SnapshotID: "snapshot.platform", Revision: 3, Audience: []string{"portal:acme:operations"}, IssuedAt: now, NotBefore: now, ExpiresAt: now.Add(10 * time.Minute), Policy: policy}}
}

func mustJSON(value any) []byte { raw, _ := json.Marshal(value); return raw }
