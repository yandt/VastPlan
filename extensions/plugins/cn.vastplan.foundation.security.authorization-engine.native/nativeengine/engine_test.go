package nativeengine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestEngineReturnsBoundedDecisionProof(t *testing.T) {
	now := time.Now().UTC()
	policy := testPolicy(now)
	digest, err := authorizationv1.AuthorizationIRDigest(policy)
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngine()
	prepared, err := engine.Prepare(authorizationv1.EnginePrepareRequest{Snapshot: authorizationv1.SignedPolicySnapshot{Payload: authorizationv1.PolicySnapshot{
		SchemaVersion: "v1", SnapshotID: "policy.1", Revision: 1, Audience: []string{"service:platform"},
		IssuedAt: now.Add(-time.Minute), NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Policy: policy,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Evaluate(authorizationv1.EngineEvaluateRequest{Handle: prepared.Handle, Input: authorizationv1.EvaluationInput{
		RequestID: "request-1", PolicyDigest: digest, DomainID: "platform.root",
		Subject: authorizationv1.Subject{Kind: authorizationv1.SubjectUser, ID: "enterprise.alice", Issuer: authenticationv1.StableSubjectIssuer},
		Target:  authorizationv1.EvaluationTarget{ExtensionPoint: "tool.package", Capability: "platform.demo", Operation: "list"},
		Scope:   authorizationv1.EvaluationScope{TenantID: "tenant-a"}, RequiredPermissions: []string{"platform.demo.read"}, EvaluatedAt: now,
	}})
	if err != nil || result.Decision != authorizationv1.DecisionAllow || result.Proof.Decision != result.Decision || result.Proof.PolicyDigest != digest || result.Proof.ValidUntil.After(now.Add(5*time.Minute+time.Second)) {
		t.Fatalf("Decision Proof 无效: result=%+v err=%v", result, err)
	}
}

func TestDescriptorMatchesProviderManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	items, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ExtensionPoint != extpoint.ToolPackage || item.ID != Capability {
			continue
		}
		var signed, runtime any
		_ = json.Unmarshal(item.Descriptor, &signed)
		_ = json.Unmarshal(Descriptor(), &runtime)
		if !equalJSON(signed, runtime) {
			t.Fatalf("Native Engine descriptor 漂移: signed=%v runtime=%v", signed, runtime)
		}
		return
	}
	t.Fatal("Manifest 缺少 Native Engine tool contribution")
}

func testPolicy(now time.Time) authorizationv1.AuthorizationIR {
	configuration := authorizationv1.ConfigurationRevisionRef{ProfileID: "authorization.native", Revision: 1, Digest: strings.Repeat("c", 64)}
	provider := authorizationv1.ProviderProfile{ID: "authorization.native", Revision: 1,
		Store:  authorizationv1.ProviderRef{Protocol: authorizationv1.ProtocolStore, ProviderID: "native-file", PluginID: "cn.vastplan.platform.security.authorization-policy", Capability: "platform.authorization.store", Version: "0.2.0", Configuration: configuration},
		Engine: authorizationv1.ProviderRef{Protocol: authorizationv1.ProtocolEngine, ProviderID: "native-rbac", PluginID: "cn.vastplan.foundation.security.authorization-engine.native", Capability: Capability, Version: "0.1.1", Configuration: configuration}, Exchange: []authorizationv1.ProviderRef{}}
	return authorizationv1.AuthorizationIR{SchemaVersion: "v1", CatalogDigest: strings.Repeat("a", 64), RootDomainID: "platform.root", ProviderProfiles: []authorizationv1.ProviderProfile{provider},
		Domains:     []authorizationv1.PolicyDomain{{ID: "platform.root", Revision: 1, Kind: authorizationv1.DomainPlatform, ProviderProfileID: provider.ID, Delegation: authorizationv1.DelegationCeiling{Permissions: []string{"platform.demo.read"}, MaxRisk: authorizationv1.RiskCritical, MaxTTLSeconds: 300}}},
		Roles:       []authorizationv1.CompiledRole{{ID: "reader", Revision: 1, DomainID: "platform.root", Statements: []authorizationv1.PolicyStatement{{ID: "allow", Effect: authorizationv1.EffectAllow, Permissions: []string{"platform.demo.read"}, Constraints: []authorizationv1.AttributeConstraint{}}}}},
		Bindings:    []authorizationv1.SubjectBinding{{ID: "alice", Revision: 1, DomainID: "platform.root", Subject: authorizationv1.Subject{Kind: authorizationv1.SubjectUser, ID: "enterprise.alice", Issuer: authenticationv1.StableSubjectIssuer}, RoleID: "reader", RoleRevision: 1, NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}},
		Revocations: []authorizationv1.Revocation{}}
}

func equalJSON(left, right any) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return string(a) == string(b)
}
