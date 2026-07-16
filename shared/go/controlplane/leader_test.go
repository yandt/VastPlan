package controlplane

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func TestLeaderElection_SingleWriterFailoverAndFencing(t *testing.T) {
	_, buckets := startControlplaneNATS(t)
	options := LeaderElectionOptions{
		LeaseDuration: 400 * time.Millisecond, RenewEvery: 50 * time.Millisecond, RetryEvery: 20 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first, err := (LeaderElector{KV: buckets.Controllers, Election: "deployment/acme", Identity: "controller-a", Options: options}).Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	secondResult := make(chan *Leadership, 1)
	secondErrors := make(chan error, 1)
	go func() {
		leadership, acquireErr := (LeaderElector{KV: buckets.Controllers, Election: "deployment/acme", Identity: "controller-b", Options: options}).Acquire(ctx)
		if acquireErr != nil {
			secondErrors <- acquireErr
			return
		}
		secondResult <- leadership
	}()
	select {
	case leadership := <-secondResult:
		_ = leadership.Close(context.Background())
		t.Fatal("第一任 leader 仍存活时不得出现第二写者")
	case err := <-secondErrors:
		t.Fatalf("第二候选者异常退出: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	firstRecord := first.Record()
	if err := first.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	var second *Leadership
	select {
	case second = <-secondResult:
	case err := <-secondErrors:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("第一任 leader 释放后第二候选者未接管")
	}
	defer func() { _ = second.Close(context.Background()) }()
	if second.Record().Token == firstRecord.Token || second.Record().Holder != "controller-b" {
		t.Fatalf("接任 leader 必须获得新 fencing token: first=%+v second=%+v", firstRecord, second.Record())
	}

	// 旧持有者使用旧 revision 释放时不得删除新 leader 的记录。
	if err := buckets.Controllers.Delete(context.Background(), "leaders."+keyToken("deployment/acme"), jetstream.LastRevision(1)); err == nil {
		t.Fatal("过期 revision 不得删除当前领导权")
	}
	if entry, err := buckets.Controllers.Get(context.Background(), "leaders."+keyToken("deployment/acme")); err != nil || entry == nil {
		t.Fatalf("fencing 后当前 leader 记录应仍存在: %v", err)
	}
}

func startControlplaneNATS(t *testing.T) (*natsserver.Server, Buckets) {
	t.Helper()
	server, err := natsserver.NewServer(&natsserver.Options{
		JetStream: true, StoreDir: filepath.Join(t.TempDir(), "jetstream"),
		Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	go server.Start()
	if !server.ReadyForConnections(10 * time.Second) {
		t.Fatal("嵌入式 NATS 未就绪")
	}
	if _, ok := server.Addr().(*net.TCPAddr); !ok {
		t.Fatal("嵌入式 NATS 未监听 TCP")
	}
	t.Cleanup(func() { server.Shutdown(); server.WaitForShutdown() })
	nc, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	buckets, err := EnsureBuckets(context.Background(), js, 1, jetstream.MemoryStorage)
	if err != nil {
		t.Fatal(err)
	}
	return server, buckets
}
