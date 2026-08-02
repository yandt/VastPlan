package authorizationpolicy

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
)

type readableMemoryWriter struct {
	snapshot authorizationv1.SignedPolicySnapshot
}

func (w *readableMemoryWriter) Read() (authorizationv1.SignedPolicySnapshot, error) {
	return w.snapshot, nil
}
func (w *readableMemoryWriter) Write(snapshot authorizationv1.SignedPolicySnapshot) error {
	w.snapshot = snapshot
	return nil
}

func testLeasePolicy(t *testing.T) SnapshotLeasePolicy {
	t.Helper()
	lease, err := NewFixedSnapshotLeasePolicy(SnapshotLeasePolicyOptions{
		Audiences: []string{"service:platform"}, SnapshotTTL: 5 * time.Minute, RenewalLead: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return lease
}

func TestFixedSnapshotLeasePolicyRenewsOnlyConfiguredBindings(t *testing.T) {
	now := time.Date(2026, 8, 2, 1, 0, 0, 0, time.UTC)
	lease, err := NewFixedSnapshotLeasePolicy(SnapshotLeasePolicyOptions{
		Audiences: []string{"portal:local:operations"}, SnapshotTTL: 5 * time.Minute, RenewalLead: time.Minute,
		ManagedBindingCreators: []string{"seed-authority"}, ManagedBindingTTL: 24 * time.Hour, ManagedBindingRenewalLead: 6 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	state := State{Bindings: []BindingRevision{
		{ID: "seed", Revision: 1, State: StatePublished, CreatedBy: "seed-authority", ExpiresAt: now.Add(time.Hour)},
		{ID: "user", Revision: 1, State: StatePublished, CreatedBy: "operator", ExpiresAt: now.Add(time.Hour)},
	}}
	renewed := lease.RenewManagedBindings(&state, now)
	if len(renewed) != 1 || renewed[0] != "seed" || !state.Bindings[0].ExpiresAt.Equal(now.Add(24*time.Hour)) || !state.Bindings[1].ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("Managed Binding 续签越界: renewed=%v bindings=%+v", renewed, state.Bindings)
	}
}

func TestServiceRepairsSnapshotProjectionFromAuthoritativeState(t *testing.T) {
	now := time.Date(2026, 8, 2, 1, 0, 0, 0, time.UTC)
	catalog := testCatalog(t)
	profile := NativeProviderProfile(catalog)
	root, _ := RootDomain(catalog, profile)
	state, err := BuildBootstrapState(catalog, profile, []authorizationv1.PolicyDomain{root}, []BootstrapGrant{{
		RoleID: "platform.owner", Title: "Owner", SubjectID: "owner", PermissionSelectors: exactPermissionSelectors([]string{"platform.demo.read"}),
	}}, now)
	if err != nil {
		t.Fatal(err)
	}
	lease := testLeasePolicy(t)
	snapshot, err := CompileSnapshot(state, lease.Audiences(), now, lease.SnapshotTTL())
	if err != nil {
		t.Fatal(err)
	}
	state.CurrentSnapshot = &snapshot
	store := &memoryStore{state: state}
	writer := &readableMemoryWriter{}
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := NewService(ServiceOptions{
		Store: store, Signer: Ed25519Signer{KeyID: "policy.1", Private: private}, SnapshotWriter: writer,
		Catalog: catalog, ProviderProfile: profile, Domains: []authorizationv1.PolicyDomain{root}, LeasePolicy: lease, Now: func() time.Time { return now },
	}); err != nil {
		t.Fatal(err)
	}
	committed, _ := store.Load()
	if committed.Generation != state.Generation || writer.snapshot.Payload.SnapshotID != snapshot.SnapshotID || !sameStrings(writer.snapshot.Payload.Audience, lease.Audiences()) {
		t.Fatalf("投影修复不得产生业务 revision: state=%+v snapshot=%+v", committed, writer.snapshot.Payload)
	}
}

func TestServiceRenewsExpiredSnapshotAndManagedBindingThroughOneAuthority(t *testing.T) {
	now := time.Date(2026, 8, 2, 1, 0, 0, 0, time.UTC)
	catalog := testCatalog(t)
	profile := NativeProviderProfile(catalog)
	root, _ := RootDomain(catalog, profile)
	issuedAt := now.Add(-10 * time.Minute)
	state, err := BuildBootstrapState(catalog, profile, []authorizationv1.PolicyDomain{root}, []BootstrapGrant{{
		RoleID: "platform.seed-owner", Title: "Seed Owner", SubjectID: "seed", PermissionSelectors: exactPermissionSelectors([]string{"platform.demo.read"}),
	}}, issuedAt)
	if err != nil {
		t.Fatal(err)
	}
	state.Bindings[0].ExpiresAt = now.Add(time.Minute)
	expired, _ := CompileSnapshot(state, []string{"development:local"}, issuedAt, 5*time.Minute)
	state.CurrentSnapshot = &expired
	lease, err := NewFixedSnapshotLeasePolicy(SnapshotLeasePolicyOptions{
		Audiences: []string{"development:local", "portal:local:operations"}, SnapshotTTL: 5 * time.Minute, RenewalLead: time.Minute,
		ManagedBindingCreators: []string{"seed-authority"}, ManagedBindingTTL: 24 * time.Hour, ManagedBindingRenewalLead: 6 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{state: state}
	writer := &readableMemoryWriter{}
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := NewService(ServiceOptions{
		Store: store, Signer: Ed25519Signer{KeyID: "policy.1", Private: private}, SnapshotWriter: writer,
		Catalog: catalog, ProviderProfile: profile, Domains: []authorizationv1.PolicyDomain{root}, LeasePolicy: lease, Now: func() time.Time { return now },
	}); err != nil {
		t.Fatal(err)
	}
	committed, _ := store.Load()
	if committed.Generation != state.Generation+1 || committed.PolicyRevision != state.PolicyRevision+1 || !committed.Bindings[0].ExpiresAt.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("租约协调未通过同一权威 CAS: %+v", committed)
	}
	if !sameStrings(writer.snapshot.Payload.Audience, lease.Audiences()) || !writer.snapshot.Payload.ExpiresAt.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("续签快照未使用统一 Lease Policy: %+v", writer.snapshot.Payload)
	}
}

func TestSnapshotAuthorityDriftRequiresExplicitBootstrapPolicy(t *testing.T) {
	now := time.Date(2026, 8, 2, 1, 0, 0, 0, time.UTC)
	catalog := testCatalog(t)
	profile := NativeProviderProfile(catalog)
	root, _ := RootDomain(catalog, profile)
	state, err := BuildBootstrapState(catalog, profile, []authorizationv1.PolicyDomain{root}, []BootstrapGrant{{
		RoleID: "platform.owner", Title: "Owner", SubjectID: "owner", PermissionSelectors: exactPermissionSelectors([]string{"platform.demo.read"}),
	}}, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	lease := testLeasePolicy(t)
	old, _ := CompileSnapshot(state, lease.Audiences(), now.Add(-time.Hour), lease.SnapshotTTL())
	state.CurrentSnapshot = &old
	state.PolicyRevision++
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	options := ServiceOptions{
		Store: &memoryStore{state: state}, Signer: Ed25519Signer{KeyID: "policy.1", Private: private}, SnapshotWriter: &readableMemoryWriter{},
		Catalog: catalog, ProviderProfile: profile, Domains: []authorizationv1.PolicyDomain{root}, LeasePolicy: lease, Now: func() time.Time { return now },
	}
	if _, err := NewService(options); err == nil || !strings.Contains(err.Error(), "显式 Bootstrap") {
		t.Fatalf("普通启动不得隐式修复权威漂移: %v", err)
	}
	store := &memoryStore{state: state}
	writer := &readableMemoryWriter{}
	options.Store, options.SnapshotWriter, options.BootstrapState, options.BootstrapReconciliation = store, writer, &state, SeedOwnedBootstrapReconciliation{}
	if _, err := NewService(options); err != nil {
		t.Fatalf("显式 Bootstrap 应可重新签发权威快照: %v", err)
	}
	recovered, _ := store.Load()
	if recovered.PolicyRevision != state.PolicyRevision+1 || recovered.CurrentSnapshot == nil || recovered.CurrentSnapshot.Revision != recovered.PolicyRevision || writer.snapshot.Payload.Revision != recovered.PolicyRevision {
		t.Fatalf("权威漂移未原子恢复: state=%+v projection=%+v", recovered, writer.snapshot.Payload)
	}
}
