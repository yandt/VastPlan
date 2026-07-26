package enforcer

import (
	"context"
	"strings"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type staticSource struct {
	bundle PolicyBundle
	err    error
}

func (s *staticSource) Load() (PolicyBundle, error) { return s.bundle, s.err }

type staticDirectory struct {
	groups   []authorizationv1.ExternalGroup
	revision uint64
}

func (d staticDirectory) Groups(string) ([]authorizationv1.ExternalGroup, uint64, error) {
	return d.groups, d.revision, nil
}

func TestEnforcerDirectAndGroupBindings(t *testing.T) {
	now := time.Date(2026, 7, 23, 1, 0, 0, 0, time.UTC)
	bundle := testBundle(t, now)
	checker, err := New(&staticSource{bundle: bundle}, staticDirectory{groups: []authorizationv1.ExternalGroup{{ID: "ops", Issuer: "https://id.example"}}, revision: 7}, []string{"service:platform"})
	if err != nil {
		t.Fatal(err)
	}
	checker.now = func() time.Time { return now }
	callCtx := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "enterprise.alice"}, Principal: &contractv1.Principal{UserId: "enterprise.alice"}, TenantId: "tenant-a"}
	raw := []byte(`{"extensionPoint":"tool.package","capability":"platform.demo","operation":"list"}`)
	response, err := checker.Check(context.Background(), callCtx, raw)
	if err != nil || response.Decision != extpoint.DecisionAllow {
		t.Fatalf("绑定用户应允许: response=%+v err=%v", response, err)
	}
	callCtx.Principal.UserId, callCtx.Caller.Id = "enterprise.bob", "enterprise.bob"
	response, err = checker.Check(context.Background(), callCtx, raw)
	if err != nil || response.Decision != extpoint.DecisionAllow {
		t.Fatalf("外部组绑定应允许: response=%+v err=%v", response, err)
	}
	response, err = checker.Check(context.Background(), callCtx, []byte(`{"extensionPoint":"tool.package","capability":"platform.other","operation":"list"}`))
	if err != nil || response.Decision != extpoint.DecisionAbstain {
		t.Fatalf("未知操作应 abstain: %+v %v", response, err)
	}
}

func TestEnforcerFailsClosedOnRevocationAndAudience(t *testing.T) {
	now := time.Date(2026, 7, 23, 1, 0, 0, 0, time.UTC)
	bundle := testBundle(t, now)
	bundle.Snapshot.Payload.Policy.Revocations = []authorizationv1.Revocation{{ID: "r1", Revision: 1, Kind: "subject", TargetID: "enterprise.alice", EffectiveAt: now.Add(-time.Second), ReasonCode: "security"}}
	bundle.Snapshot.Payload.Policy.RevocationRevision = 1
	checker, _ := New(&staticSource{bundle: bundle}, EmptyGroupDirectory{}, []string{"service:platform"})
	checker.now = func() time.Time { return now }
	ctx := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "enterprise.alice"}, Principal: &contractv1.Principal{UserId: "enterprise.alice"}, TenantId: "tenant-a"}
	response, _ := checker.Check(context.Background(), ctx, []byte(`{"extensionPoint":"tool.package","capability":"platform.demo","operation":"list"}`))
	if response.Decision != extpoint.DecisionDeny {
		t.Fatalf("撤权必须拒绝: %+v", response)
	}
	checker, _ = New(&staticSource{bundle: testBundle(t, now)}, EmptyGroupDirectory{}, []string{"service:other"})
	checker.now = func() time.Time { return now }
	response, _ = checker.Check(context.Background(), ctx, []byte(`{"extensionPoint":"tool.package","capability":"platform.demo","operation":"list"}`))
	if response.Decision != extpoint.DecisionDeny {
		t.Fatalf("audience 不匹配必须拒绝: %+v", response)
	}
}

func testBundle(t *testing.T, now time.Time) PolicyBundle {
	t.Helper()
	catalog := pluginv1.PermissionCatalog{SchemaVersion: pluginv1.PermissionCatalogSchemaVersion, Permissions: []pluginv1.PermissionCatalogEntry{{PermissionDeclaration: pluginv1.PermissionDeclaration{Code: "platform.demo.read", Title: "Read", Scope: "platform", Risk: "high", Assignable: true}, PluginID: "cn.vastplan.platform.demo", PluginVersion: "1.0.0", Publisher: "vastplan", ArtifactSHA256: strings.Repeat("a", 64)}}, Operations: []pluginv1.PermissionOperationEntry{{OperationGuard: pluginv1.OperationGuard{ExtensionPoint: "tool.package", Capability: "platform.demo", Operation: "list", Permissions: []string{"platform.demo.read"}, Access: "read", Approval: "none"}, PluginID: "cn.vastplan.platform.demo", PluginVersion: "1.0.0", ArtifactSHA256: strings.Repeat("a", 64)}}}
	digest, err := pluginv1.PermissionCatalogDigest(catalog)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Digest = digest
	configuration := authorizationv1.ConfigurationRevisionRef{ProfileID: "authorization.native", Revision: 1, Digest: strings.Repeat("c", 64)}
	provider := authorizationv1.ProviderProfile{ID: "authorization.native", Revision: 1,
		Store:  authorizationv1.ProviderRef{Protocol: authorizationv1.ProtocolStore, ProviderID: "kernel-shared-state", PluginID: "cn.vastplan.platform.security.authorization-policy", Capability: "platform.authorization.store", Version: "0.2.1", Configuration: configuration},
		Engine: authorizationv1.ProviderRef{Protocol: authorizationv1.ProtocolEngine, ProviderID: "native-rbac", PluginID: "cn.vastplan.foundation.security.authorization-engine.native", Capability: "foundation.security.authorization-engine.native", Version: PluginVersion, Configuration: configuration}, Exchange: []authorizationv1.ProviderRef{}}
	role := authorizationv1.CompiledRole{ID: "reader", Revision: 1, DomainID: "platform.root", Statements: []authorizationv1.PolicyStatement{{ID: "allow", Effect: authorizationv1.EffectAllow, Permissions: []string{"platform.demo.read"}, Constraints: []authorizationv1.AttributeConstraint{}}}}
	policy := authorizationv1.AuthorizationIR{SchemaVersion: "v1", CatalogDigest: digest, RootDomainID: "platform.root", ProviderProfiles: []authorizationv1.ProviderProfile{provider}, Domains: []authorizationv1.PolicyDomain{{ID: "platform.root", Revision: 1, Kind: authorizationv1.DomainPlatform, ProviderProfileID: provider.ID, Delegation: authorizationv1.DelegationCeiling{Permissions: []string{"platform.demo.read"}, MaxRisk: authorizationv1.RiskCritical, MayDelegate: true, MaxTTLSeconds: 300}}}, Roles: []authorizationv1.CompiledRole{role}, Bindings: []authorizationv1.SubjectBinding{{ID: "alice", Revision: 1, DomainID: "platform.root", Subject: authorizationv1.Subject{Kind: authorizationv1.SubjectUser, ID: "enterprise.alice", Issuer: authenticationv1.StableSubjectIssuer}, RoleID: "reader", RoleRevision: 1, NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}, {ID: "ops", Revision: 1, DomainID: "platform.root", Subject: authorizationv1.Subject{Kind: authorizationv1.SubjectGroup, ID: "ops", Issuer: "https://id.example"}, RoleID: "reader", RoleRevision: 1, NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}}, Revocations: []authorizationv1.Revocation{}}
	return PolicyBundle{Catalog: catalog, Snapshot: authorizationv1.SignedPolicySnapshot{Payload: authorizationv1.PolicySnapshot{SchemaVersion: "v1", SnapshotID: "policy.1", Revision: 1, Audience: []string{"service:platform"}, IssuedAt: now.Add(-time.Minute), NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Policy: policy}}}
}
