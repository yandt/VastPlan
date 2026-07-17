//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	addressing "cdsoft.com.cn/VastPlan/shared/go/addressing"
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
	if second.Record().Epoch < firstRecord.Epoch || second.Record().Token == firstRecord.Token {
		t.Fatalf("接管必须产生不降低的 epoch 和新 token: first=%+v second=%+v", firstRecord, second.Record())
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
