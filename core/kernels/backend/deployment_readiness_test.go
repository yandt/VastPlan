package main

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
)

func TestNATSDeploymentReadinessFencesRevisions(t *testing.T) {
	buckets := startReadinessNATS(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	observer := natsDeploymentReadiness{KV: buckets.Compositions, now: func() time.Time { return now }}

	missing, err := observer.Observe(ctx, "acme", "prod", 9)
	if err != nil || missing.Status != deploymentpublication.ReadinessPending || missing.Reason != "report_missing" {
		t.Fatalf("缺失报告必须保持 Pending: observation=%+v err=%v", missing, err)
	}

	putReadiness(t, buckets.Compositions, deploymentpublication.ReadinessObservation{
		SchemaVersion: 1, Tenant: "acme", Deployment: "prod", Revision: 8, Generation: 4,
		Status: deploymentpublication.ReadinessReady, UpdatedAt: now.Add(-time.Second),
	})
	stale, err := observer.Observe(ctx, "acme", "prod", 9)
	if err != nil || stale.Status != deploymentpublication.ReadinessPending || stale.Reason != "revision_pending" || stale.Generation != 4 {
		t.Fatalf("旧 revision 不得被当作新发布就绪: observation=%+v err=%v", stale, err)
	}

	exactReport := deploymentpublication.ReadinessObservation{
		SchemaVersion: 1, Tenant: "acme", Deployment: "prod", Revision: 9, Generation: 5,
		Status: deploymentpublication.ReadinessReady, UpdatedAt: now,
	}
	putReadiness(t, buckets.Compositions, exactReport)
	exact, err := observer.Observe(ctx, "acme", "prod", 9)
	if err != nil || exact.Status != deploymentpublication.ReadinessReady || exact.Revision != 9 {
		t.Fatalf("精确 revision 应返回真实报告: observation=%+v err=%v", exact, err)
	}

	putReadiness(t, buckets.Compositions, deploymentpublication.ReadinessObservation{
		SchemaVersion: 1, Tenant: "acme", Deployment: "prod", Revision: 10, Generation: 6,
		Status: deploymentpublication.ReadinessReady, UpdatedAt: now,
	})
	superseded, err := observer.Observe(ctx, "acme", "prod", 9)
	if err != nil || superseded.Status != deploymentpublication.ReadinessFailed || superseded.Reason != "revision_superseded" {
		t.Fatalf("被更新 revision 越过必须 fail-closed: observation=%+v err=%v", superseded, err)
	}
}

func putReadiness(t *testing.T, kv jetstream.KeyValue, report deploymentpublication.ReadinessObservation) {
	t.Helper()
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kv.Put(context.Background(), controlplane.CompositionKey(report.Tenant, report.Deployment), raw); err != nil {
		t.Fatal(err)
	}
}

func startReadinessNATS(t *testing.T) controlplane.Buckets {
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
	t.Cleanup(func() {
		server.Shutdown()
		server.WaitForShutdown()
	})
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
	return buckets
}
