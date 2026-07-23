package platformcatalog

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	server "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	sharedcontrolplane "cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
)

func TestStoreFallsBackSeedsOnceAndReadsDurableSnapshot(t *testing.T) {
	ctx := context.Background()
	serverInstance, buckets := startCatalogNATS(t)
	defer serverInstance.Shutdown()
	seed := testCatalog(1)
	store, err := NewStore(buckets.BackendPlatformCatalogs, seed)
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := store.Snapshot(ctx)
	if err != nil || fallback.Digest() != seed.Digest() {
		t.Fatalf("缺少持久快照时未使用已验证 Seed: catalog=%+v err=%v", fallback, err)
	}
	firstRevision, err := store.Seed(ctx)
	if err != nil || firstRevision == 0 {
		t.Fatalf("初始 Seed 未持久化: revision=%d err=%v", firstRevision, err)
	}
	secondRevision, err := store.Seed(ctx)
	if err != nil || secondRevision != firstRevision {
		t.Fatalf("Seed 重试必须幂等: first=%d second=%d err=%v", firstRevision, secondRevision, err)
	}

	next := testCatalog(2)
	raw, err := encodeSnapshot(next)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := buckets.BackendPlatformCatalogs.Get(ctx, store.key)
	if _, err := buckets.BackendPlatformCatalogs.Update(ctx, store.key, raw, entry.Revision()); err != nil {
		t.Fatal(err)
	}
	active, err := store.Snapshot(ctx)
	if err != nil || active.Revision != 2 || active.Digest() != next.Digest() {
		t.Fatalf("未读取新的持久活动快照: catalog=%+v err=%v", active, err)
	}

	tampered := persistedSnapshot{SchemaVersion: schemaVersion, Catalog: next, Digest: strings.Repeat("f", 64)}
	tamperedRaw, _ := json.Marshal(tampered)
	entry, _ = buckets.BackendPlatformCatalogs.Get(ctx, store.key)
	if _, err := buckets.BackendPlatformCatalogs.Update(ctx, store.key, tamperedRaw, entry.Revision()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Snapshot(ctx); err == nil {
		t.Fatal("持久快照损坏时不得静默回退到 Seed")
	}
}

func testCatalog(revision uint64) backendcompositionv1.BackendPlatformCatalog {
	profile := backendcompositionv1.PlatformProfile{
		Document: compositioncommonv1.Document{Version: 1, Revision: revision, ID: "backend-default"},
		Target:   compositioncommonv1.Target{Kernel: compositioncommonv1.KernelBackend}, ServiceClasses: []string{"application.backend"},
		Attachments: []backendcompositionv1.Attachment{}, Services: []deploymentv2.ServiceUnit{},
	}
	return backendcompositionv1.BackendPlatformCatalog{
		Document: compositioncommonv1.Document{Version: 1, Revision: revision, ID: "backend-production"}, Profiles: []backendcompositionv1.PlatformProfile{profile},
		Bindings: []backendcompositionv1.BackendPlatformBinding{{TenantID: "acme", DeploymentName: "services", PlatformProfile: compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()}}},
	}
}

func startCatalogNATS(t *testing.T) (*server.Server, sharedcontrolplane.Buckets) {
	t.Helper()
	instance, err := server.NewServer(&server.Options{JetStream: true, StoreDir: t.TempDir(), Port: -1})
	if err != nil {
		t.Fatal(err)
	}
	instance.Start()
	if !instance.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS 未就绪")
	}
	connection, err := nats.Connect(instance.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(connection.Close)
	js, err := jetstream.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	buckets, err := sharedcontrolplane.EnsureBuckets(context.Background(), js, 1, jetstream.MemoryStorage)
	if err != nil {
		t.Fatal(err)
	}
	return instance, buckets
}
