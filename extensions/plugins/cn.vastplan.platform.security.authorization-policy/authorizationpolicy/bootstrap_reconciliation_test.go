package authorizationpolicy

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
)

func TestServiceReconcilesSeedOwnedBootstrapIntoExistingState(t *testing.T) {
	catalog := testCatalog(t)
	profile := NativeProviderProfile(catalog)
	root, err := RootDomain(catalog, profile)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 1, 7, 0, 0, 0, time.UTC)
	owner := BootstrapGrant{RoleID: "platform.owner", Title: "Owner", SubjectID: "local-admin", PermissionSelectors: exactPermissionSelectors([]string{"platform.demo.read"})}
	current, err := BuildBootstrapState(catalog, profile, []authorizationv1.PolicyDomain{root}, []BootstrapGrant{owner}, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	current.Generation = 7
	bootstrap, err := BuildBootstrapState(catalog, profile, []authorizationv1.PolicyDomain{root}, []BootstrapGrant{
		owner,
		{RoleID: "platform.seed-owner", Title: "Seed Owner", SubjectID: "seed-user-1", PermissionSelectors: []PermissionSelector{{Kind: PermissionSelectorGlob, Value: "platform.**"}}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{state: current}
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	writer := &memoryWriter{}
	service, err := NewService(ServiceOptions{
		Store: store, BootstrapState: &bootstrap, BootstrapReconciliation: SeedOwnedBootstrapReconciliation{},
		Signer: Ed25519Signer{KeyID: "policy.1", Private: private}, SnapshotWriter: writer,
		Catalog: catalog, ProviderProfile: profile, Domains: []authorizationv1.PolicyDomain{root}, LeasePolicy: testLeasePolicy(t), Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	state, _ := store.Load()
	if state.Generation != 8 || !hasRole(state, "platform.seed-owner") || !hasBinding(state, "platform.seed-owner.binding") {
		t.Fatalf("显式协调未原子补齐 Shared State: generation=%d roles=%v bindings=%v", state.Generation, state.Roles, state.Bindings)
	}
	if len(state.Audit) == 0 || state.Audit[len(state.Audit)-1].Action != "bootstrapSeedReconcile" {
		t.Fatalf("Seed 协调必须留下独立审计: %+v", state.Audit)
	}
	if state.PolicyRevision != current.PolicyRevision+1 || state.CurrentSnapshot == nil || writer.snapshot.Payload.Policy.CatalogDigest != catalog.Digest || len(writer.snapshot.Payload.Policy.Roles) != 2 {
		t.Fatalf("Seed 协调必须同步发布完整 Snapshot: policyRevision=%d snapshot=%+v", state.PolicyRevision, writer.snapshot.Payload)
	}
	if err := service.initialize(store); err != nil {
		t.Fatal(err)
	}
	idempotent, _ := store.Load()
	if idempotent.Generation != state.Generation {
		t.Fatalf("已收敛 Seed 基线不得反复产生 generation: %d -> %d", state.Generation, idempotent.Generation)
	}
}

func TestServiceDoesNotReconcileBootstrapByDefault(t *testing.T) {
	catalog := testCatalog(t)
	profile := NativeProviderProfile(catalog)
	root, _ := RootDomain(catalog, profile)
	now := time.Date(2026, 8, 1, 7, 0, 0, 0, time.UTC)
	owner := BootstrapGrant{RoleID: "platform.owner", Title: "Owner", SubjectID: "local-admin", PermissionSelectors: exactPermissionSelectors([]string{"platform.demo.read"})}
	current, _ := BuildBootstrapState(catalog, profile, []authorizationv1.PolicyDomain{root}, []BootstrapGrant{owner}, now)
	bootstrap, _ := BuildBootstrapState(catalog, profile, []authorizationv1.PolicyDomain{root}, []BootstrapGrant{owner, {
		RoleID: "platform.seed-owner", Title: "Seed Owner", SubjectID: "seed-user-1", PermissionSelectors: exactPermissionSelectors([]string{"platform.demo.read"}),
	}}, now)
	store := &memoryStore{state: current}
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := NewService(ServiceOptions{Store: store, BootstrapState: &bootstrap, Signer: Ed25519Signer{KeyID: "policy.1", Private: private}, SnapshotWriter: &memoryWriter{}, Catalog: catalog, ProviderProfile: profile, Domains: []authorizationv1.PolicyDomain{root}, LeasePolicy: testLeasePolicy(t)}); err != nil {
		t.Fatal(err)
	}
	state, _ := store.Load()
	if hasRole(state, "platform.seed-owner") || state.Generation != current.Generation {
		t.Fatalf("普通启动不得吸收 Bootstrap Grant: %+v", state)
	}
}

func TestSeedOwnedBootstrapReconciliationRejectsReservedCollision(t *testing.T) {
	catalog := testCatalog(t)
	profile := NativeProviderProfile(catalog)
	root, _ := RootDomain(catalog, profile)
	now := time.Date(2026, 8, 1, 7, 0, 0, 0, time.UTC)
	bootstrap, _ := BuildBootstrapState(catalog, profile, []authorizationv1.PolicyDomain{root}, []BootstrapGrant{{
		RoleID: "platform.seed-owner", Title: "Seed Owner", SubjectID: "seed-user-1", PermissionSelectors: exactPermissionSelectors([]string{"platform.demo.read"}),
	}}, now)
	current := bootstrap
	current.Roles = append([]RoleRevision(nil), bootstrap.Roles...)
	current.Roles[0].CreatedBy = "local-admin"
	_, err := (SeedOwnedBootstrapReconciliation{}).Reconcile(&current, &bootstrap, catalog.Digest, now)
	if err == nil || !strings.Contains(err.Error(), "非受信定义占用") {
		t.Fatalf("保留对象冲突必须 fail closed: %v", err)
	}
}

func hasRole(state State, id string) bool {
	for _, role := range state.Roles {
		if role.ID == id {
			return true
		}
	}
	return false
}

func hasBinding(state State, id string) bool {
	for _, binding := range state.Bindings {
		if binding.ID == id {
			return true
		}
	}
	return false
}
