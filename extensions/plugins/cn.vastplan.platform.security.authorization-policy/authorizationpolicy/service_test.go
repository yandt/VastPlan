package authorizationpolicy

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"sync"
	"testing"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type memoryStore struct {
	mu    sync.Mutex
	state State
}

func (s *memoryStore) Load() (State, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.state, nil }
func (s *memoryStore) CompareAndSwap(expected uint64, next State) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Generation != expected {
		return State{}, errCAS
	}
	s.state = next
	return next, nil
}

var errCAS = &testError{"cas"}

type testError struct{ value string }

func (e *testError) Error() string { return e.value }

type memoryWriter struct {
	snapshot authorizationv1.SignedPolicySnapshot
}

func (w *memoryWriter) Write(snapshot authorizationv1.SignedPolicySnapshot) error {
	w.snapshot = snapshot
	return nil
}

func TestRoleBindingApprovalAndSignedSnapshot(t *testing.T) {
	catalog := testCatalog(t)
	profile := NativeProviderProfile(catalog)
	root, err := RootDomain(catalog, profile)
	if err != nil {
		t.Fatal(err)
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	store, writer := &memoryStore{state: State{Version: stateVersion}}, &memoryWriter{}
	now := time.Date(2026, 7, 23, 1, 2, 3, 0, time.UTC)
	service, err := NewService(ServiceOptions{Store: store, Signer: Ed25519Signer{KeyID: "policy.1", Private: private}, SnapshotWriter: writer, Catalog: catalog, ProviderProfile: profile, Domains: []authorizationv1.PolicyDomain{root}, DefaultAudience: []string{"service:platform"}, DefaultTTL: 5 * time.Minute, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	statement := authorizationv1.PolicyStatement{ID: "allow-read", Effect: authorizationv1.EffectAllow, Permissions: []string{"platform.demo.read"}, Constraints: []authorizationv1.AttributeConstraint{}}
	if _, err := service.createRole("alice", CreateRoleRequest{ExpectedGeneration: 1, ID: "platform.reader", DomainID: "platform.root", Title: "Reader", Statements: []authorizationv1.PolicyStatement{statement}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.transitionRole("alice", "submitRole", TransitionRequest{ExpectedGeneration: 2, ID: "platform.reader", Revision: 1}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.transitionRole("alice", "approveRole", TransitionRequest{ExpectedGeneration: 3, ID: "platform.reader", Revision: 1}, nil); err == nil {
		t.Fatal("创建人不得审批自己的 Role")
	}
	if _, err := service.transitionRole("bob", "approveRole", TransitionRequest{ExpectedGeneration: 3, ID: "platform.reader", Revision: 1}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.transitionRole("bob", "publishRole", TransitionRequest{ExpectedGeneration: 4, ID: "platform.reader", Revision: 1}, nil); err != nil {
		t.Fatal(err)
	}
	binding := CreateBindingRequest{ExpectedGeneration: 5, ID: "alice-reader", DomainID: "platform.root", Subject: authorizationv1.Subject{Kind: authorizationv1.SubjectUser, ID: "enterprise.alice", Issuer: authenticationv1.StableSubjectIssuer}, RoleID: "platform.reader", RoleRevision: 1, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	if _, err := service.createBinding("alice", binding, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.updateBinding("alice", UpdateBindingRequest{ExpectedGeneration: 6, ID: "alice-reader", Revision: 1, DomainID: binding.DomainID, Subject: binding.Subject, RoleID: binding.RoleID, RoleRevision: binding.RoleRevision, NotBefore: binding.NotBefore, ExpiresAt: now.Add(2 * time.Hour)}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.transitionBinding("alice", "submitBinding", TransitionRequest{ExpectedGeneration: 7, ID: "alice-reader", Revision: 1}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.transitionBinding("bob", "approveBinding", TransitionRequest{ExpectedGeneration: 8, ID: "alice-reader", Revision: 1}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.transitionBinding("bob", "publishBinding", TransitionRequest{ExpectedGeneration: 9, ID: "alice-reader", Revision: 1}, nil); err != nil {
		t.Fatal(err)
	}
	value, err := service.publishSnapshot("bob", PublishSnapshotRequest{ExpectedGeneration: 10, Reason: "initial policy"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if value == nil || writer.snapshot.Payload.Revision != 1 || writer.snapshot.Payload.Policy.CatalogDigest != catalog.Digest || len(writer.snapshot.Payload.Policy.Roles) != 1 || len(writer.snapshot.Payload.Policy.Bindings) != 1 {
		t.Fatalf("发布快照不完整: %+v", writer.snapshot.Payload)
	}
	canonical, err := authorizationv1.CanonicalPolicySnapshot(writer.snapshot.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(private.Public().(ed25519.PublicKey), canonical, mustDecodeSignature(t, writer.snapshot.Signature.Value)) {
		t.Fatal("Policy Snapshot 签名无效")
	}
	if _, err := service.revoke("bob", RevokeRequest{ExpectedGeneration: 11, ID: "security-incident-1", Kind: "binding", TargetID: "alice-reader", EffectiveAt: now, ReasonCode: "security_incident"}, nil); err != nil {
		t.Fatal(err)
	}
	if writer.snapshot.Payload.Revision != 2 || writer.snapshot.Payload.Policy.RevocationRevision != 1 || len(writer.snapshot.Payload.Policy.Revocations) != 1 {
		t.Fatalf("撤权必须同步签发新快照: %+v", writer.snapshot.Payload)
	}
}

func testCatalog(t *testing.T) pluginv1.PermissionCatalog {
	t.Helper()
	catalog := pluginv1.PermissionCatalog{SchemaVersion: pluginv1.PermissionCatalogSchemaVersion, Permissions: []pluginv1.PermissionCatalogEntry{{PermissionDeclaration: pluginv1.PermissionDeclaration{Code: "platform.demo.read", Title: "Read", Scope: "platform", Risk: "high", Assignable: true}, PluginID: "cn.vastplan.platform.demo", PluginVersion: "1.0.0", Publisher: "vastplan", ArtifactSHA256: strings.Repeat("a", 64)}}, Operations: []pluginv1.PermissionOperationEntry{{OperationGuard: pluginv1.OperationGuard{ExtensionPoint: "tool.package", Capability: "platform.demo", Operation: "list", Permissions: []string{"platform.demo.read"}, Access: "read", Approval: "none"}, PluginID: "cn.vastplan.platform.demo", PluginVersion: "1.0.0", ArtifactSHA256: strings.Repeat("a", 64)}}}
	digest, err := pluginv1.PermissionCatalogDigest(catalog)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Digest = digest
	return catalog
}

func mustDecodeSignature(t *testing.T, value string) []byte {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
