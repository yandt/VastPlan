package sharedstate

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func TestNATSStoreCrossInstanceCASIsolationAndPagination(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	kv := testKV(t, ctx)
	first, _ := NewNATSStore(kv)
	second, _ := NewNATSStore(kv)
	tenantA := Scope{Kind: ScopeTenant, TenantID: "tenant-a", PluginID: "cn.vastplan.settings", RuntimeScope: "platform-settings", Namespace: "values"}
	tenantB := tenantA
	tenantB.TenantID = "tenant-b"

	created, err := first.Create(ctx, tenantA, "settings.theme", []byte(`{"mode":"light"}`))
	if err != nil || created.Revision == 0 {
		t.Fatalf("create: entry=%+v err=%v", created, err)
	}
	observed, err := second.Get(ctx, tenantA, "settings.theme")
	if err != nil || string(observed.Value) != `{"mode":"light"}` {
		t.Fatalf("第二实例未观察到状态: entry=%+v err=%v", observed, err)
	}
	updated, err := second.Update(ctx, tenantA, "settings.theme", []byte(`{"mode":"dark"}`), observed.Revision)
	if err != nil || updated.Revision <= observed.Revision {
		t.Fatalf("CAS update: entry=%+v err=%v", updated, err)
	}
	if _, err := first.Update(ctx, tenantA, "settings.theme", []byte(`{"mode":"stale"}`), observed.Revision); !errors.Is(err, ErrConflict) {
		t.Fatalf("旧 revision 必须被 fencing: %v", err)
	}
	if _, err := first.Get(ctx, tenantB, "settings.theme"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨 tenant 读取必须隔离: %v", err)
	}
	if _, err := first.Create(ctx, tenantA, "settings.locale", []byte(`"zh-CN"`)); err != nil {
		t.Fatal(err)
	}
	page, err := first.List(ctx, tenantA, "settings.", 1, "")
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("第一页异常: %+v err=%v", page, err)
	}
	next, err := first.List(ctx, tenantA, "settings.", 1, page.NextCursor)
	if err != nil || len(next.Items) != 1 || next.Items[0].Key == page.Items[0].Key {
		t.Fatalf("第二页异常: %+v err=%v", next, err)
	}
	if err := first.Delete(ctx, tenantA, "settings.theme", observed.Revision); !errors.Is(err, ErrConflict) {
		t.Fatalf("旧 revision delete 必须冲突: %v", err)
	}
	if err := second.Delete(ctx, tenantA, "settings.theme", updated.Revision); err != nil {
		t.Fatal(err)
	}
}

func testKV(t *testing.T, ctx context.Context) jetstream.KeyValue {
	t.Helper()
	srv, err := server.NewServer(&server.Options{JetStream: true, StoreDir: filepath.Join(t.TempDir(), "js"), Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS 未就绪")
	}
	if _, ok := srv.Addr().(*net.TCPAddr); !ok {
		t.Fatal("NATS 未监听 TCP")
	}
	t.Cleanup(func() { srv.Shutdown(); srv.WaitForShutdown() })
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	kv, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "VASTPLAN_SHARED_STATE_TEST", History: 16, Storage: jetstream.MemoryStorage, MaxValueSize: MaxValueBytes})
	if err != nil {
		t.Fatal(err)
	}
	return kv
}
