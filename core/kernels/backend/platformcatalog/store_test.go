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

func TestStoreReadsAndCASUpgradesValidatedSchemaV1Seed(t *testing.T) {
	ctx := context.Background()
	serverInstance, buckets := startCatalogNATS(t)
	defer serverInstance.Shutdown()
	seed := testCatalog(1)
	store, err := NewStore(buckets.BackendPlatformCatalogs, seed)
	if err != nil {
		t.Fatal(err)
	}
	legacyRaw := legacyCatalogSnapshotRaw(t, seed, 1)
	var legacyWire persistedSnapshot
	if err := json.Unmarshal(legacyRaw, &legacyWire); err != nil {
		t.Fatal(err)
	}
	contractUpgrade, legacyDigest, err := backendcompositionv1.UpgradeLegacyBackendPlatformCatalog(legacyWire.Catalog)
	if err != nil || legacyDigest != legacyWire.Digest || contractUpgrade.Digest() != seed.Digest() {
		t.Fatalf("契约迁移器未保持新旧 Catalog 身份: old=%s stored=%s new=%s want=%s err=%v", legacyDigest, legacyWire.Digest, contractUpgrade.Digest(), seed.Digest(), err)
	}
	if parsed, err := parseSnapshot(legacyRaw); err != nil || parsed.Digest != seed.Digest() {
		t.Fatalf("持久快照解析器未采用契约迁移结果: parsed=%+v err=%v", parsed, err)
	}
	if _, err := buckets.BackendPlatformCatalogs.Create(ctx, store.key, legacyRaw); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := store.Snapshot(ctx); err != nil || snapshot.Digest() != seed.Digest() {
		t.Fatalf("缺少 productCapabilities 的已验证 v1 Seed 应可只读迁移: snapshot=%+v err=%v", snapshot, err)
	}
	if _, err := store.Seed(ctx); err != nil {
		t.Fatalf("Bootstrap 应以 CAS 持久升级 v1 Seed: %v", err)
	}
	entry, err := buckets.BackendPlatformCatalogs.Get(ctx, store.key)
	if err != nil {
		t.Fatal(err)
	}
	var upgraded persistedSnapshot
	if err := json.Unmarshal(entry.Value(), &upgraded); err != nil || upgraded.SchemaVersion != schemaVersion {
		t.Fatalf("v1 Seed 未升级为当前 schema: state=%+v err=%v", upgraded, err)
	}
	if len(upgraded.Catalog.Profiles) != 1 || upgraded.Catalog.Profiles[0].ProductCapabilities == nil || upgraded.Digest != upgraded.Catalog.Digest() {
		t.Fatalf("v1 Seed 未补齐当前 Catalog 与摘要: state=%+v", upgraded)
	}
	var drifted map[string]any
	if err := json.Unmarshal(legacyRaw, &drifted); err != nil {
		t.Fatal(err)
	}
	drifted["digest"] = strings.Repeat("0", 64)
	driftedRaw, err := json.Marshal(drifted)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseSnapshot(driftedRaw); err == nil {
		t.Fatal("旧版 Catalog 的历史摘要漂移不得迁移")
	}

	tampered := persistedSnapshot{SchemaVersion: 1, Catalog: seed, Digest: strings.Repeat("f", 64), Candidate: &Candidate{}}
	tamperedRaw, _ := json.Marshal(tampered)
	if _, err := buckets.BackendPlatformCatalogs.Update(ctx, store.key, tamperedRaw, entry.Revision()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Snapshot(ctx); err == nil {
		t.Fatal("带未知候选或错误摘要的 v1 快照不得迁移")
	}
}

func TestStoreCASUpgradesLegacyCatalogInsideSchemaV3(t *testing.T) {
	ctx := context.Background()
	serverInstance, buckets := startCatalogNATS(t)
	defer serverInstance.Shutdown()
	seed := testCatalog(1)
	store, err := NewStore(buckets.BackendPlatformCatalogs, seed)
	if err != nil {
		t.Fatal(err)
	}
	legacyRaw := legacyCatalogSnapshotRaw(t, seed, schemaVersion)
	if _, err := buckets.BackendPlatformCatalogs.Create(ctx, store.key, legacyRaw); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := store.Snapshot(ctx); err != nil || snapshot.Digest() != seed.Digest() {
		t.Fatalf("v3 外壳中的历史 Catalog 应可只读迁移: snapshot=%+v err=%v", snapshot, err)
	}
	if _, err := store.Seed(ctx); err != nil {
		t.Fatalf("Bootstrap 应以 CAS 持久升级 v3 外壳中的历史 Catalog: %v", err)
	}
	entry, err := buckets.BackendPlatformCatalogs.Get(ctx, store.key)
	if err != nil {
		t.Fatal(err)
	}
	var upgraded persistedSnapshot
	if err := json.Unmarshal(entry.Value(), &upgraded); err != nil || upgraded.SchemaVersion != schemaVersion || upgraded.Digest != upgraded.Catalog.Digest() {
		t.Fatalf("历史 Catalog 未规范升级: state=%+v err=%v", upgraded, err)
	}
	if len(upgraded.Catalog.Profiles) != 1 || upgraded.Catalog.Profiles[0].ProductCapabilities == nil {
		t.Fatalf("历史 Catalog 未显式补齐 Product Capability: %+v", upgraded.Catalog.Profiles)
	}
}

type testLegacyPlatformProfile struct {
	compositioncommonv1.Document
	Target           compositioncommonv1.Target             `json:"target"`
	ServiceClasses   []string                               `json:"serviceClasses"`
	ServiceBaselines []backendcompositionv1.ServiceBaseline `json:"serviceBaselines"`
	Services         []deploymentv2.ServiceUnit             `json:"services"`
}

type testLegacyBackendPlatformCatalog struct {
	compositioncommonv1.Document
	Profiles []testLegacyPlatformProfile                   `json:"profiles"`
	Bindings []backendcompositionv1.BackendPlatformBinding `json:"bindings"`
}

func legacyCatalogSnapshotRaw(t *testing.T, catalog backendcompositionv1.BackendPlatformCatalog, version int) []byte {
	t.Helper()
	legacy := testLegacyBackendPlatformCatalog{
		Document: catalog.Document,
		Profiles: make([]testLegacyPlatformProfile, 0, len(catalog.Profiles)),
		Bindings: append([]backendcompositionv1.BackendPlatformBinding(nil), catalog.Bindings...),
	}
	for _, profile := range catalog.Profiles {
		legacyProfile := testLegacyPlatformProfile{
			Document: profile.Document, Target: profile.Target, ServiceClasses: profile.ServiceClasses,
			ServiceBaselines: profile.ServiceBaselines, Services: profile.Services,
		}
		legacy.Profiles = append(legacy.Profiles, legacyProfile)
		for index := range legacy.Bindings {
			ref := &legacy.Bindings[index].PlatformProfile
			if ref.ID == legacyProfile.ID && ref.Revision == legacyProfile.Revision {
				ref.Digest = compositioncommonv1.Digest(legacyProfile)
			}
		}
	}
	raw, err := json.Marshal(struct {
		SchemaVersion int                              `json:"schemaVersion"`
		Catalog       testLegacyBackendPlatformCatalog `json:"catalog"`
		Digest        string                           `json:"digest"`
	}{SchemaVersion: version, Catalog: legacy, Digest: compositioncommonv1.Digest(legacy)})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func testCatalog(revision uint64) backendcompositionv1.BackendPlatformCatalog {
	profile := backendcompositionv1.PlatformProfile{
		Document: compositioncommonv1.Document{Version: 1, Revision: revision, ID: "backend-default"},
		Target:   compositioncommonv1.Target{Kernel: compositioncommonv1.KernelBackend}, ServiceClasses: []string{"application.backend"},
		ServiceBaselines: []backendcompositionv1.ServiceBaseline{}, Services: []deploymentv2.ServiceUnit{},
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
