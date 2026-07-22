package configurationcatalog

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	sharedcontrolplane "cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

func TestStorePublishesOnlyCatalogMatchingActiveDeployment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	server := startJetStream(t)
	nc, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	buckets, err := sharedcontrolplane.EnsureBuckets(ctx, js, 1, jetstream.MemoryStorage)
	if err != nil {
		t.Fatal(err)
	}
	store := Store{KV: buckets.Deployments}
	first := testDeployment(1)
	publishDeployment(t, ctx, buckets.Deployments, first)
	firstCatalog, err := pluginconfiguration.Build(first, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(ctx, "tenant-a", firstCatalog); err != nil {
		t.Fatal(err)
	}
	items, err := store.List(ctx, "tenant-a")
	if err != nil || len(items) != 1 || items[0].Digest != firstCatalog.Digest {
		t.Fatalf("配置目录发布后不可读: items=%+v err=%v", items, err)
	}

	second := testDeployment(2)
	publishDeployment(t, ctx, buckets.Deployments, second)
	items, err = store.List(ctx, "tenant-a")
	if err != nil || len(items) != 0 {
		t.Fatalf("Deployment 已推进时必须隐藏旧目录: items=%+v err=%v", items, err)
	}
	if err := store.Publish(ctx, "tenant-a", firstCatalog); err == nil {
		t.Fatal("不得重新发布与活动 Deployment 不匹配的旧目录")
	}
	secondCatalog, err := pluginconfiguration.Build(second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(ctx, "tenant-a", secondCatalog); err != nil {
		t.Fatal(err)
	}
	items, err = store.List(ctx, "tenant-a")
	if err != nil || len(items) != 1 || items[0].DeploymentRevision != 2 {
		t.Fatalf("新目录未替换旧目录: items=%+v err=%v", items, err)
	}
	other, err := store.List(ctx, "tenant-b")
	if err != nil || len(other) != 0 {
		t.Fatalf("配置目录必须按 tenant 隔离: items=%+v err=%v", other, err)
	}
}

func testDeployment(revision uint64) deploymentv2.Deployment {
	return deploymentv2.Deployment{
		Version: 2, Revision: revision, Metadata: deploymentv1.Metadata{Name: "platform", Tenant: "tenant-a"}, Units: []deploymentv2.ServiceUnit{},
		Resolution: deploymentv2.Resolution{
			PlatformProfile:        compositioncommonv1.Ref{ID: "platform", Revision: revision, Digest: strings.Repeat("a", 64)},
			ApplicationComposition: compositioncommonv1.Ref{ID: "application", Revision: revision, Digest: strings.Repeat("b", 64)},
			PluginOrigins:          map[string]string{},
		},
	}
}

func publishDeployment(t *testing.T, ctx context.Context, kv jetstream.KeyValue, deployment deploymentv2.Deployment) {
	t.Helper()
	raw, err := json.Marshal(deployment)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := sharedcontrolplane.ApplyDeployment(ctx, kv, sharedcontrolplane.DeploymentKey(deployment.Metadata.Tenant, deployment.Metadata.Name), raw); err != nil {
		t.Fatal(err)
	}
}

func startJetStream(t *testing.T) *natsserver.Server {
	t.Helper()
	server, err := natsserver.NewServer(&natsserver.Options{JetStream: true, StoreDir: filepath.Join(t.TempDir(), "jetstream"), Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	server.Start()
	if !server.ReadyForConnections(5 * time.Second) {
		server.Shutdown()
		t.Fatal("NATS 未就绪")
	}
	t.Cleanup(func() { server.Shutdown(); server.WaitForShutdown() })
	return server
}
