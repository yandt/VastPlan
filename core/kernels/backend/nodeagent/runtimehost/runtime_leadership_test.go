package runtimehost

import (
	"context"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go/jetstream"
)

func TestProtocolRuntimeLeadershipOutlivesReconcileContext(t *testing.T) {
	ns := startNATSServer(t, server.Options{JetStream: true, StoreDir: t.TempDir(), Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	defer func() { ns.Shutdown(); ns.WaitForShutdown() }()
	nc, err := controlplane.Connect(ns.ClientURL(), "runtime-leadership-test", t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	buckets, err := controlplane.EnsureBuckets(context.Background(), js, 1, jetstream.MemoryStorage)
	if err != nil {
		t.Fatal(err)
	}
	runtime := NewProtocolRuntime("1.0.0", t.Logf)
	runtime.Identity = "node-a"
	runtime.LeaderKV = buckets.Controllers
	unit := RuntimeUnit{
		ID: "platform-credentials", LogicalService: "platform.credentials", InstancePolicy: "leader",
		StateModel: "external-shared", Visibility: "cluster", Routing: "leader", RoutingDomain: "platform",
	}
	policy, err := UnitPolicy(deploymentUnitForRuntime(unit))
	if err != nil {
		t.Fatal(err)
	}
	reconcileCtx, cancelReconcile := context.WithTimeout(context.Background(), time.Second)
	owned, leaderships, err := runtime.acquireUnitLeaderships(reconcileCtx, unit, policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(leaderships) != 1 {
		t.Fatalf("leadership 数量=%d want=1", len(leaderships))
	}
	defer func() { _ = leaderships[0].Close(context.Background()) }()
	cancelReconcile()
	time.Sleep(120 * time.Millisecond)
	current := &runningUnit{spec: owned, leaderships: leaderships}
	if ready, issue := leadershipReadiness(current); !ready {
		t.Fatalf("reconcile 结束后 fencing 应保持就绪: %s", issue)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-leaderships[0].Done():
	case <-time.After(time.Second):
		t.Fatal("Runtime 关闭后 leadership 未停止")
	}
	if ready, _ := leadershipReadiness(current); ready {
		t.Fatal("Runtime 关闭后 readiness 不得继续报告 leader fencing 就绪")
	}
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
