package references

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactreference"
)

func TestStoreEnforcesOwnerGenerationLeaseAndRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repository")
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	value := sealed(t, 1, 60, true)
	if _, revision, err := store.Put("tenant-a", "cn.vastplan.deployment", value, now); err != nil || revision != 1 {
		t.Fatalf("put failed: revision=%d err=%v", revision, err)
	}
	value.References[0].Purpose = "mutated-after-put"
	_, listed := store.List()
	if listed[0].Value.References[0].Purpose != "active" {
		t.Fatal("caller-owned slices must not alias persisted reference state")
	}
	value = sealed(t, 1, 60, true)
	drift := value
	drift.Digest = strings.Repeat("f", 64)
	if _, _, err := store.Put("tenant-a", "cn.vastplan.deployment", drift, now); err == nil {
		t.Fatal("same-generation drift must fail")
	}
	if health := store.Health([]RequiredOwner{{TenantID: "tenant-a", OwnerKind: value.OwnerKind, OwnerID: value.OwnerID}}, now.Add(61*time.Second)); health.Ready || len(health.Expired) != 1 {
		t.Fatalf("expired owner must block GC health: %+v", health)
	}
	if len(store.Protected()) != 1 {
		t.Fatal("expired snapshots must continue protecting bytes")
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if revision, snapshots := reopened.List(); revision != 1 || len(snapshots) != 1 {
		t.Fatalf("restart lost reference state: revision=%d snapshots=%+v", revision, snapshots)
	}
	released := sealed(t, 2, 60, false)
	if _, revision, err := reopened.Put("tenant-a", "cn.vastplan.deployment", released, now.Add(time.Minute)); err != nil || revision != 2 || len(reopened.Protected()) != 0 {
		t.Fatalf("empty higher generation must release protection: revision=%d err=%v", revision, err)
	}
}

func TestStoreRejectsPublisherTakeoverAndReportsMissingOwners(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repository")
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	value := sealed(t, 1, 0, true)
	if _, _, err := store.Put("tenant-a", "publisher-a", value, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	value = sealed(t, 2, 0, true)
	if _, _, err := store.Put("tenant-a", "publisher-b", value, time.Now().UTC()); err == nil {
		t.Fatal("another publisher must not take over an owner key")
	}
	health := store.Health([]RequiredOwner{{TenantID: "tenant-a", OwnerKind: artifactreference.OwnerSeed, OwnerID: "platform/seed"}}, time.Now().UTC())
	if health.Ready || len(health.Missing) != 1 {
		t.Fatalf("missing required owner must block GC: %+v", health)
	}
}

func TestStorePreservesEmptySnapshotDigestAcrossRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repository")
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	value, err := artifactreference.Seal(pluginv1.ArtifactReferenceSnapshot{OwnerKind: artifactreference.OwnerSeed, OwnerID: "seed/primary", Generation: 1, References: []pluginv1.ArtifactReference{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Put("system", "bootstrap-inventory/primary", value, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatalf("空引用快照必须保留 [] 与摘要语义: %v", err)
	}
	_, snapshots := reopened.List()
	if len(snapshots) != 1 || snapshots[0].Value.References == nil {
		t.Fatalf("空引用快照不应退化为 null: %+v", snapshots)
	}
}

func sealed(t *testing.T, generation uint64, ttl uint32, withReference bool) pluginv1.ArtifactReferenceSnapshot {
	t.Helper()
	value := pluginv1.ArtifactReferenceSnapshot{OwnerKind: artifactreference.OwnerAssignmentActive, OwnerID: "platform/node-a", Generation: generation, TTLSeconds: ttl, References: []pluginv1.ArtifactReference{}}
	if withReference {
		value.References = append(value.References, pluginv1.ArtifactReference{Ref: pluginv1.ArtifactRef{PluginID: "cn.example.api", Version: "1.0.0", Channel: "stable"}, SHA256: strings.Repeat("a", 64), Purpose: "active"})
	}
	sealed, err := artifactreference.Seal(value)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}
