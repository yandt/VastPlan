package portalcomposer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func TestComposerStateSupportsMultiChunkSnapshot(t *testing.T) {
	host := newStateOnlyHost(t)
	repository, err := newComposerStateRepository(host)
	if err != nil {
		t.Fatal(err)
	}
	call := &contractv1.CallContext{TenantId: "tenant-a"}
	value := emptyState()
	value.Audit = []portalapi.AuditEvent{{ID: 1, TenantID: "tenant-a", PortalID: "admin", Action: "capacity.test", ActorID: "test", Reason: strings.Repeat("x", 2<<20), Priority: "normal", At: "2026-07-30T00:00:00Z"}}
	revision, err := repository.save(context.Background(), call, value, 0)
	if err != nil {
		t.Fatalf("超过单值上限的治理历史应拆成内容寻址 chunk: %v", err)
	}
	rootEntry, err := repository.client.Get(context.Background(), call, composerStateKey)
	if err != nil {
		t.Fatal(err)
	}
	root, err := decodeComposerRoot(rootEntry.Value)
	if err != nil || len(root.Chunks) < 4 || len(rootEntry.Value) >= 1<<20 {
		t.Fatalf("Root 必须保持小型且引用多个 chunk: root=%+v bytes=%d err=%v", root, len(rootEntry.Value), err)
	}
	loaded, loadedRevision, err := repository.load(context.Background(), call)
	if err != nil || loadedRevision != revision || loaded.Audit[0].Reason != value.Audit[0].Reason {
		t.Fatalf("多 chunk 快照跨读取丢失: revision=%d err=%v", loadedRevision, err)
	}
}

func TestComposerStateMigratesLegacySingleDocumentOnNextCAS(t *testing.T) {
	host := newStateOnlyHost(t)
	repository, err := newComposerStateRepository(host)
	if err != nil {
		t.Fatal(err)
	}
	call := &contractv1.CallContext{TenantId: "tenant-a"}
	legacy := emptyState()
	raw, _ := json.Marshal(legacy)
	entry, err := repository.client.Create(context.Background(), call, composerStateKey, raw)
	if err != nil {
		t.Fatal(err)
	}
	loaded, revision, err := repository.load(context.Background(), call)
	if err != nil || revision != entry.Revision {
		t.Fatalf("旧单文档必须保持可读: revision=%d err=%v", revision, err)
	}
	if _, err := repository.save(context.Background(), call, loaded, revision); err != nil {
		t.Fatalf("旧单文档迁移 Root 失败: %v", err)
	}
	migrated, err := repository.client.Get(context.Background(), call, composerStateKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeComposerRoot(migrated.Value); err != nil {
		t.Fatalf("下一次 CAS 必须把旧文档原位升级为 Root: %v", err)
	}
}

func TestComposerStateRejectsTamperedChunk(t *testing.T) {
	host := newStateOnlyHost(t)
	repository, err := newComposerStateRepository(host)
	if err != nil {
		t.Fatal(err)
	}
	call := &contractv1.CallContext{TenantId: "tenant-a"}
	if _, err := repository.save(context.Background(), call, emptyState(), 0); err != nil {
		t.Fatal(err)
	}
	rootEntry, _ := repository.client.Get(context.Background(), call, composerStateKey)
	root, _ := decodeComposerRoot(rootEntry.Value)
	chunk, _ := repository.client.Get(context.Background(), call, composerBlobPrefix+root.Chunks[0].Digest)
	if _, err := repository.client.Update(context.Background(), call, chunk.Key, []byte("tampered"), chunk.Revision); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.load(context.Background(), call); err == nil || !strings.Contains(err.Error(), "chunk") {
		t.Fatalf("摘要不匹配的 chunk 必须 fail-closed: %v", err)
	}
}
