package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstate"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.credentials/credentialsstate"
)

func TestChunkGCTwoPhaseGraceDeletesOnlyOrphan(t *testing.T) {
	host := newCredentialStateHost(t)
	repository, _ := newCredentialStateRepository(host)
	call := credentialContext("tenant-a")
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	first := snapshotWithNamedCiphertext(now, "vault:v1:first")
	firstRevision, err := repository.save(context.Background(), call, first, 0)
	if err != nil {
		t.Fatal(err)
	}
	firstRoot := readCredentialRoot(t, host, "tenant-a")
	second := snapshotWithNamedCiphertext(now.Add(time.Minute), "vault:v1:second")
	if _, err := repository.save(context.Background(), call, second, firstRevision); err != nil {
		t.Fatal(err)
	}
	secondRoot := readCredentialRoot(t, host, "tenant-a")
	if firstRoot.Chunks[0].Digest == secondRoot.Chunks[0].Digest {
		t.Fatal("测试快照必须产生不同 chunk")
	}
	policy := testChunkGCPolicy()
	if err := repository.collectOrphanChunks(context.Background(), call, now, policy); err != nil {
		t.Fatal(err)
	}
	if _, err := host.store.Get(context.Background(), host.scope("tenant-a"), credentialsstate.GCMarkerKey(firstRoot.Chunks[0].Digest)); err != nil {
		t.Fatalf("首次 mark 必须持久化 orphan marker: %v", err)
	}
	if err := repository.collectOrphanChunks(context.Background(), call, now, policy); err != nil {
		t.Fatal(err)
	}
	if _, err := host.store.Get(context.Background(), host.scope("tenant-a"), credentialsBlobPrefix+firstRoot.Chunks[0].Digest); err != nil {
		t.Fatalf("宽限期内不得删除 orphan chunk: %v", err)
	}
	afterGrace := now.Add(policy.OrphanChunkGrace + policy.Interval)
	if err := repository.collectOrphanChunks(context.Background(), call, afterGrace, policy); err != nil {
		t.Fatal(err)
	}
	if err := repository.collectOrphanChunks(context.Background(), call, afterGrace, policy); err != nil {
		t.Fatal(err)
	}
	if _, err := host.store.Get(context.Background(), host.scope("tenant-a"), credentialsBlobPrefix+firstRoot.Chunks[0].Digest); !errors.Is(err, sharedstate.ErrNotFound) {
		t.Fatalf("宽限期后 orphan chunk 未删除: %v", err)
	}
	if _, err := host.store.Get(context.Background(), host.scope("tenant-a"), credentialsBlobPrefix+secondRoot.Chunks[0].Digest); err != nil {
		t.Fatalf("当前 Root chunk 被误删: %v", err)
	}
	loaded, _, err := repository.load(context.Background(), call)
	if err != nil || loaded.Records["primary"].Ciphertext != "vault:v1:second" {
		t.Fatalf("GC 后当前快照不可读: value=%+v err=%v", loaded, err)
	}
}

func TestChunkGCRootRecheckProtectsDigestThatBecameReachable(t *testing.T) {
	host := newCredentialStateHost(t)
	repository, _ := newCredentialStateRepository(host)
	call := credentialContext("tenant-a")
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	firstRevision, _ := repository.save(context.Background(), call, snapshotWithNamedCiphertext(now, "vault:v1:first"), 0)
	firstRootEntry, _ := host.store.Get(context.Background(), host.scope("tenant-a"), credentialsRootKey)
	firstRoot, _ := credentialsstate.ParseRoot(firstRootEntry.Value)
	secondRevision, _ := repository.save(context.Background(), call, snapshotWithNamedCiphertext(now.Add(time.Minute), "vault:v1:second"), firstRevision)
	policy := testChunkGCPolicy()
	if err := repository.collectOrphanChunks(context.Background(), call, now, policy); err != nil {
		t.Fatal(err)
	}
	// Simulate a later valid commit reusing the immutable old content before
	// sweep. The marker must be removed, while the chunk remains.
	if _, err := repository.writer.Update(context.Background(), call, credentialsRootKey, firstRootEntry.Value, secondRevision); err != nil {
		t.Fatal(err)
	}
	if err := repository.collectOrphanChunks(context.Background(), call, now.Add(policy.OrphanChunkGrace), policy); err != nil {
		t.Fatal(err)
	}
	if _, err := host.store.Get(context.Background(), host.scope("tenant-a"), credentialsBlobPrefix+firstRoot.Chunks[0].Digest); err != nil {
		t.Fatalf("重新可达的 chunk 被误删: %v", err)
	}
	if _, err := host.store.Get(context.Background(), host.scope("tenant-a"), credentialsstate.GCMarkerKey(firstRoot.Chunks[0].Digest)); !errors.Is(err, sharedstate.ErrNotFound) {
		t.Fatalf("重新可达的 marker 未清理: %v", err)
	}
}

func TestChunkGCSweepRecoversAfterBlobWasAlreadyDeleted(t *testing.T) {
	host := newCredentialStateHost(t)
	repository, _ := newCredentialStateRepository(host)
	call := credentialContext("tenant-a")
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	revision, _ := repository.save(context.Background(), call, snapshotWithNamedCiphertext(now, "vault:v1:current"), 0)
	_ = revision
	digest := credentialsstate.DigestHex([]byte("orphan"))
	blob, _ := repository.writer.Create(context.Background(), call, credentialsBlobPrefix+digest, []byte("orphan"))
	marker, _ := credentialsstate.NewChunkGCMarker(digest, blob.Revision, now.Add(-2*time.Hour))
	markerRaw, _ := json.Marshal(marker)
	markerEntry, _ := repository.writer.Create(context.Background(), call, credentialsstate.GCMarkerKey(digest), markerRaw)
	state := credentialsstate.ChunkGCState{Format: credentialsstate.GCStateFormat, Phase: credentialsstate.GCPhaseSweep, CycleStartedAt: timePointer(now.Add(-2 * time.Hour))}
	stateRaw, _ := json.Marshal(state)
	if _, err := repository.writer.Create(context.Background(), call, credentialsstate.GCStateKey, stateRaw); err != nil {
		t.Fatal(err)
	}
	if err := repository.writer.Delete(context.Background(), call, credentialsBlobPrefix+digest, blob.Revision); err != nil {
		t.Fatal(err)
	}
	if err := repository.collectOrphanChunks(context.Background(), call, now, testChunkGCPolicy()); err != nil {
		t.Fatal(err)
	}
	if _, err := host.store.Get(context.Background(), host.scope("tenant-a"), credentialsstate.GCMarkerKey(digest)); !errors.Is(err, sharedstate.ErrNotFound) {
		t.Fatalf("崩溃恢复未清理悬空 marker revision=%d: %v", markerEntry.Revision, err)
	}
}

func TestChunkGCMarkIsBoundedAndResumesByCursor(t *testing.T) {
	host := newCredentialStateHost(t)
	repository, _ := newCredentialStateRepository(host)
	call := credentialContext("tenant-a")
	for _, value := range [][]byte{[]byte("orphan-a"), []byte("orphan-b"), []byte("orphan-c")} {
		digest := credentialsstate.DigestHex(value)
		if _, err := repository.writer.Create(context.Background(), call, credentialsBlobPrefix+digest, value); err != nil {
			t.Fatal(err)
		}
	}
	policy := testChunkGCPolicy()
	policy.ChunkGCBatchSize = 1
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	if err := repository.collectOrphanChunks(context.Background(), call, now, policy); err != nil {
		t.Fatal(err)
	}
	if got := countCredentialKeys(t, host, "tenant-a", credentialsstate.GCMarkerPrefix); got != 1 {
		t.Fatalf("单次 mark 必须受 batch=1 限制: %d", got)
	}
	stateEntry, _ := host.store.Get(context.Background(), host.scope("tenant-a"), credentialsstate.GCStateKey)
	state, _ := credentialsstate.ParseChunkGCState(stateEntry.Value)
	if state.Phase != credentialsstate.GCPhaseMark || state.Cursor == "" {
		t.Fatalf("未完成 mark 必须保存 cursor: %+v", state)
	}
	if err := repository.collectOrphanChunks(context.Background(), call, now, policy); err != nil {
		t.Fatal(err)
	}
	if got := countCredentialKeys(t, host, "tenant-a", credentialsstate.GCMarkerPrefix); got != 2 {
		t.Fatalf("第二次请求应从 cursor 继续且仍只处理一条: %d", got)
	}
}

func TestChunkGCBlobRevisionChangeResetsGrace(t *testing.T) {
	host := newCredentialStateHost(t)
	repository, _ := newCredentialStateRepository(host)
	call := credentialContext("tenant-a")
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	value := []byte("orphan")
	digest := credentialsstate.DigestHex(value)
	blob, _ := repository.writer.Create(context.Background(), call, credentialsBlobPrefix+digest, value)
	stale, _ := credentialsstate.NewChunkGCMarker(digest, blob.Revision+1, now.Add(-48*time.Hour))
	staleRaw, _ := json.Marshal(stale)
	if _, err := repository.writer.Create(context.Background(), call, credentialsstate.GCMarkerKey(digest), staleRaw); err != nil {
		t.Fatal(err)
	}
	if err := repository.collectOrphanChunks(context.Background(), call, now, testChunkGCPolicy()); err != nil {
		t.Fatal(err)
	}
	entry, _ := host.store.Get(context.Background(), host.scope("tenant-a"), credentialsstate.GCMarkerKey(digest))
	marker, err := credentialsstate.ParseChunkGCMarker(entry.Value)
	if err != nil || marker.BlobRevision != blob.Revision || !marker.FirstObservedAt.Equal(now) {
		t.Fatalf("blob revision 变化后未重置宽限期: marker=%+v err=%v", marker, err)
	}
}

func snapshotWithNamedCiphertext(now time.Time, ciphertext string) credentialSnapshot {
	value := emptyCredentialSnapshot()
	value.Records["primary"] = Record{Name: "primary", Version: 1, KeyVersion: "v1", CreatedAt: now, UpdatedAt: now, Ciphertext: ciphertext}
	return value
}

func readCredentialRoot(t *testing.T, host *credentialStateHost, tenantID string) credentialsstate.Root {
	t.Helper()
	entry, err := host.store.Get(context.Background(), host.scope(tenantID), credentialsRootKey)
	if err != nil {
		t.Fatal(err)
	}
	root, err := credentialsstate.ParseRoot(entry.Value)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func testChunkGCPolicy() MaintenancePolicy {
	return MaintenancePolicy{PreparingMaxAge: time.Hour, AbortedRetention: 24 * time.Hour, AuditRetention: 48 * time.Hour, Interval: time.Minute, BatchSize: 20, OrphanChunkGrace: time.Hour, ChunkGCBatchSize: 200}
}

func timePointer(value time.Time) *time.Time { return &value }

func countCredentialKeys(t *testing.T, host *credentialStateHost, tenantID, prefix string) int {
	t.Helper()
	page, err := host.store.List(context.Background(), host.scope(tenantID), prefix, 200, "")
	if err != nil {
		t.Fatal(err)
	}
	return len(page.Items)
}
