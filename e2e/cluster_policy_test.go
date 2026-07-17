//go:build e2e

package e2e

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	"cdsoft.com.cn/VastPlan/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/kernels/backend/pluginservice"
	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
	addressing "cdsoft.com.cn/VastPlan/shared/go/addressing"
	"cdsoft.com.cn/VastPlan/shared/go/artifacttrust"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/controlplane"
)

func TestClusterLeaderFailoverAndPartitionRouting(t *testing.T) {
	server := startE2ENATS(t)
	nc, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	buckets, err := controlplane.EnsureBuckets(context.Background(), js, 1, jetstream.MemoryStorage)
	if err != nil {
		t.Fatal(err)
	}
	options := controlplane.LeaderElectionOptions{LeaseDuration: 300 * time.Millisecond, RenewEvery: 80 * time.Millisecond, RetryEvery: 20 * time.Millisecond}
	first, err := (controlplane.LeaderElector{KV: buckets.Controllers, Election: "plugin/database/migration", Identity: "node-a", Options: options}).Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	firstRecord := first.Record()
	secondCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	secondDone := make(chan *controlplane.Leadership, 1)
	go func() {
		second, acquireErr := (controlplane.LeaderElector{KV: buckets.Controllers, Election: "plugin/database/migration", Identity: "node-b", Options: options}).Acquire(secondCtx)
		if acquireErr == nil {
			secondDone <- second
		}
	}()
	if err := first.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	var second *controlplane.Leadership
	select {
	case second = <-secondDone:
	case <-secondCtx.Done():
		t.Fatal("leader lease 未在旧 owner 释放后转移")
	}
	if second.Record().Epoch <= firstRecord.Epoch || second.Record().Token == firstRecord.Token {
		t.Fatalf("接管必须产生严格递增的 epoch 和新 token: first=%+v second=%+v", firstRecord, second.Record())
	}
	_ = second.Close(context.Background())

	callerNC, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	workerNC, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	caller, err := addressing.NewRouter(callerNC, buckets.Capabilities, "caller-cluster", t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := addressing.NewRouter(workerNC, buckets.Capabilities, "worker-cluster", t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = caller.Close(); _ = worker.Close(); callerNC.Close(); workerNC.Close() })
	registration, err := worker.Register(context.Background(), addressing.RegisterOptions{
		Capability: "platform.partitioned", ExtensionPoint: "tool.package", LogicalService: "platform.database", RoutingDomain: "core", PartitionKey: "tenant-a",
		InstancePolicy: "partitioned", StateModel: "partition-owned", Visibility: "cluster", Routing: "shard", UnitID: "database-a", FencingToken: "tenant-a-token",
	}, func(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte("tenant-a-owner"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registration.Close(context.Background()) })
	deadline := time.Now().Add(3 * time.Second)
	for len(caller.Instances("platform.partitioned")) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	result, payload, err := caller.Invoke(context.Background(), &contractv1.CallTarget{
		ExtensionPoint: "tool.package", Capability: "platform.partitioned", LogicalService: proto.String("platform.database"), RoutingDomain: proto.String("core"), PartitionKey: proto.String("tenant-a"),
	}, nil, nil)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || string(payload) != "tenant-a-owner" {
		t.Fatalf("分片 owner 路由失败: result=%v payload=%q err=%v", result, payload, err)
	}
}

func TestProtocolRuntimeLeaderRollingUpgradeKeepsMonotonicFencing(t *testing.T) {
	repository, err := pluginservice.NewRepository(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	publishBuiltPlugin(t, repository, "./plugins/com.vastplan.hello-world/backend", "plugins/com.vastplan.hello-world/vastplan.plugin.json")
	artifact, packageBytes, err := repository.Read(pluginv1.ArtifactRef{PluginID: "com.vastplan.hello-world", Version: "0.1.0", Channel: "stable"})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := nodeagent.NewLocalDevelopmentArtifactVerifier().Verify(
		pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel},
		artifacttrust.Envelope{Artifact: artifact, PackageBytes: packageBytes},
	)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := (nodeagent.LocalInstaller{Root: filepath.Join(t.TempDir(), "installed")}).Install(verified)
	if err != nil {
		t.Fatal(err)
	}
	for index := range installed.Contract.Contributions {
		contribution := &installed.Contract.Contributions[index]
		contribution.InstancePolicy = "leader"
		contribution.StateModel = "leader-owned"
		contribution.Visibility = "cluster"
		contribution.Routing = "leader"
	}

	server := startE2ENATS(t)
	nc, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	buckets, err := controlplane.EnsureBuckets(context.Background(), js, 1, jetstream.MemoryStorage)
	if err != nil {
		t.Fatal(err)
	}
	router, err := addressing.NewRouter(nc, buckets.Capabilities, "leader-node", t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	runtime := nodeagent.NewProtocolRuntime("0.1.0", t.Logf)
	runtime.LeaderKV = buckets.Controllers
	runtime.Identity = "leader-node"
	if err := runtime.AttachRouter(router); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	unit := nodeagent.RuntimeUnit{
		ID: "migration", Fingerprint: "leader-v1", ServiceRole: "backend", LogicalService: "platform.database",
		InstancePolicy: "leader", StateModel: "leader-owned", Visibility: "cluster", Routing: "leader",
		Plugins: []nodeagent.InstalledPlugin{installed},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := runtime.Apply(ctx, unit); err != nil {
		t.Fatal(err)
	}
	first := waitAnnouncement(t, router, "vastplan.hello")
	oldHost, _ := runtime.Host(unit.ID)
	unit.Fingerprint = "leader-v2"
	if err := runtime.Apply(ctx, unit); err != nil {
		t.Fatal(err)
	}
	second := waitAnnouncementAfter(t, router, "vastplan.hello", first.Generation)
	newHost, _ := runtime.Host(unit.ID)
	if newHost == oldHost || !runtime.IsRunning(unit.ID, "leader-v2") {
		t.Fatal("leader 滚动升级没有原子替换旧宿主")
	}
	if second.Generation <= first.Generation || second.FencingToken == first.FencingToken {
		t.Fatalf("leader 升级必须产生严格递增 epoch 和新 token: first=%+v second=%+v", first, second)
	}
}

func waitAnnouncement(t *testing.T, router *addressing.Router, capability string) addressing.Announcement {
	return waitAnnouncementAfter(t, router, capability, 0)
}

func waitAnnouncementAfter(t *testing.T, router *addressing.Router, capability string, generation uint64) addressing.Announcement {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		instances := router.Instances(capability)
		if len(instances) == 1 && instances[0].Health == "healthy" && instances[0].Generation > generation {
			return instances[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待 capability %s announcement 超时", capability)
	return addressing.Announcement{}
}
