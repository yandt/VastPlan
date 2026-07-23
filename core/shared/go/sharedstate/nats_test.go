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

func TestParsePhysicalKeyForOperationsRoundTrip(t *testing.T) {
	scopes := []Scope{
		{Kind: ScopeTenant, TenantID: "tenant-a", PluginID: "cn.vastplan.credentials", RuntimeScope: "platform-credentials", Namespace: "credentials.ledger"},
		{Kind: ScopeService, PluginID: "cn.vastplan.service", RuntimeScope: "service-a", Namespace: "state"},
	}
	for _, scope := range scopes {
		physical, err := physicalKey(scope, "root.value")
		if err != nil {
			t.Fatal(err)
		}
		decoded, key, err := ParsePhysicalKeyForOperations(physical)
		if err != nil || decoded != scope || key != "root.value" {
			t.Fatalf("physical key round trip: scope=%+v key=%q err=%v", decoded, key, err)
		}
	}
	for _, invalid := range []string{"", "v2.tenant.-.x.x.x.x", "v1.service.dGVuYW50.c2VydmljZQ.cGx1Z2lu.c3RhdGU.a2V5", "v1.tenant.bad"} {
		if _, _, err := ParsePhysicalKeyForOperations(invalid); err == nil {
			t.Fatalf("非法 physical key 未拒绝: %q", invalid)
		}
	}
}

func TestNATSStoreFailsClosedAndRecoversAfterServerRestart(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "js-restart")
	options := &server.Options{JetStream: true, StoreDir: directory, Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true}
	first, err := server.NewServer(options)
	if err != nil {
		t.Fatal(err)
	}
	go first.Start()
	if !first.ReadyForConnections(5 * time.Second) {
		t.Fatal("初始 NATS 未就绪")
	}
	port := first.Addr().(*net.TCPAddr).Port
	nc, err := nats.Connect(first.ClientURL(), nats.MaxReconnects(-1), nats.ReconnectWait(25*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, _ := jetstream.New(nc)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	kv, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "VASTPLAN_SHARED_STATE_RESTART", History: 16, Storage: jetstream.FileStorage, MaxValueSize: MaxValueBytes})
	if err != nil {
		t.Fatal(err)
	}
	store, _ := NewNATSStore(kv)
	scope := Scope{Kind: ScopeTenant, TenantID: "tenant-a", PluginID: "cn.vastplan.demo", RuntimeScope: "service-a", Namespace: "state"}
	if _, err := store.Create(ctx, scope, "active", []byte("persisted")); err != nil {
		t.Fatal(err)
	}
	first.Shutdown()
	first.WaitForShutdown()
	unavailableCtx, unavailableCancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer unavailableCancel()
	if _, err := store.Get(unavailableCtx, scope, "active"); err == nil {
		t.Fatal("NATS 中断时 Shared State 不得假装成功或回退本地")
	}

	second, err := server.NewServer(&server.Options{JetStream: true, StoreDir: directory, Host: "127.0.0.1", Port: port, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go second.Start()
	if !second.ReadyForConnections(5 * time.Second) {
		t.Fatal("重启 NATS 未就绪")
	}
	defer func() { second.Shutdown(); second.WaitForShutdown() }()
	deadline := time.Now().Add(5 * time.Second)
	for !nc.IsConnected() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !nc.IsConnected() {
		t.Fatal("NATS 客户端未重连")
	}
	recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer recoveryCancel()
	entry, err := store.Get(recoveryCtx, scope, "active")
	if err != nil || string(entry.Value) != "persisted" {
		t.Fatalf("NATS 重启后状态未恢复: entry=%+v err=%v", entry, err)
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
