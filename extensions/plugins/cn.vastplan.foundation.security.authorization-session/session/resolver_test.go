package session

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
)

type staticSnapshotStore struct {
	value authorizationv1.SignedPolicySnapshot
}

type staticGroupDirectory struct {
	groups []authorizationv1.ExternalGroup
}

func (d staticGroupDirectory) Groups(string) ([]authorizationv1.ExternalGroup, uint64, error) {
	return d.groups, 9, nil
}

func (s staticSnapshotStore) Load() (authorizationv1.SignedPolicySnapshot, error) {
	return s.value, nil
}

func TestResolverProjectsOnlyUnconditionalPermissionsForStableSubject(t *testing.T) {
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	subjectID := StableSubjectID("enterprise-users", "urn:users", "alice")
	snapshot := testSnapshot(now, subjectID)
	resolver, _ := NewResolver(staticSnapshotStore{value: snapshot})
	resolver.now = func() time.Time { return now.Add(time.Minute) }
	request := ResolveRequest{ProviderProfileID: "enterprise-users", Issuer: "urn:users", Subject: "alice", TenantID: "acme", PortalID: "operations", AMR: []string{"pwd"}, ACR: "aal1"}
	result, raw, err := resolver.resolve(context.Background(), nil, trustedContext("acme"), mustJSON(request))
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("解析失败: %+v %v", result, err)
	}
	var resolved ResolveResult
	if err := json.Unmarshal(raw, &resolved); err != nil {
		t.Fatal(err)
	}
	if resolved.SubjectID != subjectID || strings.Join(resolved.Roles, ",") != "platform.settings.read" {
		t.Fatalf("会话权限投影错误: %+v", resolved)
	}
}

func TestResolverRejectsUnboundAndUntrustedCaller(t *testing.T) {
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	resolver, _ := NewResolver(staticSnapshotStore{value: testSnapshot(now, StableSubjectID("enterprise-users", "urn:users", "alice"))})
	resolver.now = func() time.Time { return now.Add(time.Minute) }
	request := ResolveRequest{ProviderProfileID: "enterprise-users", Issuer: "urn:users", Subject: "bob", TenantID: "acme", PortalID: "operations", AMR: []string{"pwd"}, ACR: "aal1"}
	result, _, _ := resolver.resolve(context.Background(), nil, trustedContext("acme"), mustJSON(request))
	if result.Error.Code != "foundation.authorization-session.subject-unbound" {
		t.Fatalf("未绑定主体必须拒绝: %+v", result)
	}
	result, _, _ = resolver.resolve(context.Background(), nil, &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER}}, mustJSON(request))
	if result.Error.Code != "foundation.authorization-session.forbidden" {
		t.Fatalf("非可信调用必须拒绝: %+v", result)
	}
}

func TestResolverProjectsPublishedExternalGroupBinding(t *testing.T) {
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	subjectID := StableSubjectID("enterprise-users", "urn:users", "bob")
	snapshot := testSnapshot(now, "someone-else")
	snapshot.Payload.Policy.Bindings = append(snapshot.Payload.Policy.Bindings, authorizationv1.SubjectBinding{
		ID: "binding.ops", Revision: 1, DomainID: "platform.root",
		Subject: authorizationv1.Subject{Kind: authorizationv1.SubjectGroup, ID: "ops", Issuer: "https://id.example"},
		RoleID:  "platform.viewer", RoleRevision: 1, NotBefore: now, ExpiresAt: now.Add(time.Hour),
	})
	resolver, _ := NewResolverWithDirectory(staticSnapshotStore{value: snapshot}, staticGroupDirectory{groups: []authorizationv1.ExternalGroup{{ID: "ops", Issuer: "https://id.example"}}})
	resolver.now = func() time.Time { return now.Add(time.Minute) }
	request := ResolveRequest{ProviderProfileID: "enterprise-users", Issuer: "urn:users", Subject: "bob", TenantID: "acme", PortalID: "operations", AMR: []string{"pwd"}, ACR: "aal1"}
	result, raw, err := resolver.resolve(context.Background(), nil, trustedContext("acme"), mustJSON(request))
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("外部组授权解析失败: %+v %v", result, err)
	}
	var resolved ResolveResult
	if err := json.Unmarshal(raw, &resolved); err != nil {
		t.Fatal(err)
	}
	if resolved.SubjectID != subjectID || strings.Join(resolved.Roles, ",") != "platform.settings.read" {
		t.Fatalf("外部组投影错误: %+v", resolved)
	}
}

func TestFileSnapshotStoreVerifiesCanonicalSignature(t *testing.T) {
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	snapshot := testSnapshot(now, StableSubjectID("enterprise-users", "urn:users", "alice"))
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	canonical, _ := authorizationv1.CanonicalPolicySnapshot(snapshot.Payload)
	snapshot.Signature = authorizationv1.Signature{Algorithm: "Ed25519", KeyID: "policy-key.1", Value: base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, canonical))}
	directory := t.TempDir()
	snapshotPath, trustPath := filepath.Join(directory, "snapshot.json"), filepath.Join(directory, "trust.json")
	if err := os.WriteFile(snapshotPath, mustJSON(snapshot), 0o600); err != nil {
		t.Fatal(err)
	}
	trust := map[string]any{"version": 1, "keys": []map[string]string{{"keyId": "policy-key.1", "publicKey": base64.RawStdEncoding.EncodeToString(public)}}}
	if err := os.WriteFile(trustPath, mustJSON(trust), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (FileSnapshotStore{SnapshotPath: snapshotPath, TrustPath: trustPath}).Load(); err != nil {
		t.Fatal(err)
	}
	snapshot.Payload.Audience = []string{"portal:attacker:operations"}
	if err := os.WriteFile(snapshotPath, mustJSON(snapshot), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (FileSnapshotStore{SnapshotPath: snapshotPath, TrustPath: trustPath}).Load(); err == nil {
		t.Fatal("篡改 Snapshot 后必须拒绝")
	}
}

func TestDescriptorMatchesManifest(t *testing.T) {
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
		t.Fatal("descriptor 漂移")
	}
}

func testSnapshot(now time.Time, subjectID string) authorizationv1.SignedPolicySnapshot {
	configuration := authorizationv1.ConfigurationRevisionRef{ProfileID: "authorization.default", Revision: 1, Digest: strings.Repeat("c", 64)}
	provider := authorizationv1.ProviderProfile{ID: "authorization.default", Revision: 1,
		Store:  authorizationv1.ProviderRef{Protocol: authorizationv1.ProtocolStore, ProviderID: "native-store", PluginID: "cn.vastplan.foundation.security.authorization-store", Capability: "foundation.security.authorization.store", Version: "1.0.0", Configuration: configuration},
		Engine: authorizationv1.ProviderRef{Protocol: authorizationv1.ProtocolEngine, ProviderID: "native-rbac", PluginID: "cn.vastplan.foundation.security.authorization-engine", Capability: "foundation.security.authorization.engine", Version: "1.0.0", Configuration: configuration}, Exchange: []authorizationv1.ProviderRef{}}
	policy := authorizationv1.AuthorizationIR{SchemaVersion: "v1", CatalogDigest: strings.Repeat("a", 64), RootDomainID: "platform.root", ProviderProfiles: []authorizationv1.ProviderProfile{provider},
		Domains: []authorizationv1.PolicyDomain{{ID: "platform.root", Revision: 1, Kind: authorizationv1.DomainPlatform, ProviderProfileID: provider.ID, Delegation: authorizationv1.DelegationCeiling{Permissions: []string{"platform.settings.read", "platform.settings.write"}, MaxRisk: authorizationv1.RiskCritical, MayDelegate: true, MaxTTLSeconds: 3600}}},
		Roles: []authorizationv1.CompiledRole{{ID: "platform.viewer", Revision: 1, DomainID: "platform.root", Statements: []authorizationv1.PolicyStatement{
			{ID: "allow-read", Effect: authorizationv1.EffectAllow, Permissions: []string{"platform.settings.read"}, Constraints: []authorizationv1.AttributeConstraint{}},
			{ID: "scoped-write", Effect: authorizationv1.EffectAllow, Permissions: []string{"platform.settings.write"}, Resource: &authorizationv1.ResourceSelector{Type: "platform.settings", IDs: []string{"root"}, Labels: map[string][]string{}, Ownership: "any"}, Constraints: []authorizationv1.AttributeConstraint{}},
		}}}, Bindings: []authorizationv1.SubjectBinding{{ID: "binding.enterprise.alice", Revision: 1, DomainID: "platform.root", Subject: authorizationv1.Subject{Kind: authorizationv1.SubjectUser, ID: subjectID, Issuer: StableSubjectIssuer}, RoleID: "platform.viewer", RoleRevision: 1, NotBefore: now, ExpiresAt: now.Add(time.Hour)}}, Revocations: []authorizationv1.Revocation{}, RevocationRevision: 0}
	return authorizationv1.SignedPolicySnapshot{Payload: authorizationv1.PolicySnapshot{SchemaVersion: "v1", SnapshotID: "snapshot.platform.root", Revision: 7, Audience: []string{"portal:acme:operations"}, IssuedAt: now, NotBefore: now, ExpiresAt: now.Add(10 * time.Minute), Policy: policy}, Signature: authorizationv1.Signature{Algorithm: "Ed25519", KeyID: "policy-key.1", Value: strings.Repeat("A", 86)}}
}

func trustedContext(tenant string) *contractv1.CallContext {
	return &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "portal-host"}, Scene: "portal.bff", TenantId: tenant}
}
func mustJSON(value any) []byte { raw, _ := json.Marshal(value); return raw }
