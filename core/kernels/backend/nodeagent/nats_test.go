package nodeagent

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
)

func TestNATSKVSourceWatchReconnectAndActualStateReport(t *testing.T) {
	storeDir := filepath.Join(t.TempDir(), "jetstream")
	opts := server.Options{JetStream: true, StoreDir: storeDir, Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true}
	ns := startNATSServer(t, opts)
	port := ns.Addr().(*net.TCPAddr).Port

	nc, err := controlplane.Connect(ns.ClientURL(), "nodeagent-kv-test", t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	buckets, err := controlplane.EnsureBuckets(ctx, js, 1, jetstream.FileStorage)
	if err != nil {
		t.Fatal(err)
	}

	desired := desired(1, "1.0.0", true)
	desiredKey := controlplane.DesiredKey(desired.Metadata.Tenant, desired.Metadata.Name)
	firstKVRevision := applyDesired(t, ctx, buckets.Desired, desiredKey, desired)
	if same := applyDesired(t, ctx, buckets.Desired, desiredKey, desired); same != firstKVRevision {
		t.Fatalf("幂等发布不应产生新 KV revision: first=%d same=%d", firstKVRevision, same)
	}
	conflict := desired
	conflict.Units = append([]deploymentv1.Unit(nil), desired.Units...)
	conflict.Units[0].Enabled = false
	conflictRaw, _ := json.Marshal(conflict)
	if _, _, err := controlplane.ApplyDesiredState(ctx, buckets.Desired, desiredKey, conflictRaw); err == nil {
		t.Fatal("同业务 revision 不同内容必须被发布端拒绝")
	}
	source := NATSDesiredStateSource{KV: buckets.Desired, Key: desiredKey, Conn: nc}
	loaded, err := source.Load(ctx)
	if err != nil || loaded.Revision != 1 {
		t.Fatalf("NATS Load=%+v err=%v", loaded, err)
	}
	events, err := source.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	waitSourceRevision(t, events, 1)

	desired.Revision = 2
	applyDesired(t, ctx, buckets.Desired, desiredKey, desired)
	waitSourceRevision(t, events, 2)

	actualStore := NATSStateStore{KV: buckets.Actual, Key: controlplane.ActualKey("acme", "prod", "node-1")}
	actual := emptyActualState()
	actual.NodeID, actual.ObservedRevision, actual.AppliedRevision = "node-1", 2, 2
	if err := actualStore.Save(actual); err != nil {
		t.Fatal(err)
	}
	firstActualEntry, err := buckets.Actual.Get(ctx, actualStore.Key)
	if err != nil {
		t.Fatal(err)
	}
	actual.UpdatedAt = time.Now().UTC()
	if err := actualStore.Save(actual); err != nil {
		t.Fatal(err)
	}
	unchangedActualEntry, err := buckets.Actual.Get(ctx, actualStore.Key)
	if err != nil {
		t.Fatal(err)
	}
	if unchangedActualEntry.Revision() != firstActualEntry.Revision() {
		t.Fatalf("仅检查点时间变化不得写入 Actual KV: first=%d unchanged=%d", firstActualEntry.Revision(), unchangedActualEntry.Revision())
	}
	actual.Units["worker"] = UnitState{Phase: PhaseActive, Readiness: "ready"}
	if err := actualStore.Save(actual); err != nil {
		t.Fatal(err)
	}
	changedActualEntry, err := buckets.Actual.Get(ctx, actualStore.Key)
	if err != nil {
		t.Fatal(err)
	}
	if changedActualEntry.Revision() <= unchangedActualEntry.Revision() {
		t.Fatalf("实际运行事实变化必须写入 Actual KV: before=%d after=%d", unchangedActualEntry.Revision(), changedActualEntry.Revision())
	}
	loadedActual, err := actualStore.Load()
	if err != nil || loadedActual.NodeID != "node-1" || loadedActual.AppliedRevision != 2 || loadedActual.Units["worker"].Phase != PhaseActive {
		t.Fatalf("NATS actual=%+v err=%v", loadedActual, err)
	}

	// 停服再用同一 StoreDir/端口启动，验证客户端自动重连后既有 KV watcher 继续收更新。
	ns.Shutdown()
	ns.WaitForShutdown()
	opts.Port = port
	ns = startNATSServer(t, opts)
	defer func() {
		ns.Shutdown()
		ns.WaitForShutdown()
	}()
	waitNATSConnected(t, nc)
	desired.Revision = 3
	applyDesired(t, ctx, buckets.Desired, desiredKey, desired)
	waitSourceRevision(t, events, 3)
}

func startNATSServer(t *testing.T, opts server.Options) *server.Server {
	t.Helper()
	ns, err := server.NewServer(&opts)
	if err != nil {
		t.Fatal(err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(10 * time.Second) {
		ns.Shutdown()
		t.Fatal("NATS Server 未就绪")
	}
	return ns
}

func applyDesired(t *testing.T, ctx context.Context, kv jetstream.KeyValue, key string, desired deploymentv1.DesiredState) uint64 {
	t.Helper()
	raw, err := json.Marshal(desired)
	if err != nil {
		t.Fatal(err)
	}
	revision, _, err := controlplane.ApplyDesiredState(ctx, kv, key, raw)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func waitSourceRevision(t *testing.T, events <-chan SourceEvent, revision uint64) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("NATS watcher 提前关闭")
			}
			if event.Err != nil {
				t.Fatal(event.Err)
			}
			if event.Revision >= revision {
				return
			}
		case <-deadline:
			t.Fatalf("等待 NATS KV revision %d 超时", revision)
		}
	}
}

func waitNATSConnected(t *testing.T, nc *nats.Conn) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if nc.Status() == nats.CONNECTED {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("NATS 客户端未重连，status=%v err=%v", nc.Status(), nc.LastError())
}
