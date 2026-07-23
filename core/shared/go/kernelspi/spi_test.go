package kernelspi_test

import (
	"context"
	"errors"
	"testing"

	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
)

func TestMapConfigIsImmutableAndScopeFailsClosed(t *testing.T) {
	input := map[string]any{"retries": 3}
	provider, err := kernelspi.NewMapConfig(input)
	if err != nil {
		t.Fatal(err)
	}
	input["retries"] = 9
	raw, ok, err := provider.Lookup(context.Background(), "plugin.a", "retries")
	if err != nil || !ok || string(raw) != "3" {
		t.Fatalf("配置必须在构造时冻结: %s %v %v", raw, ok, err)
	}
	if err := (kernelspi.Scope{TenantID: "t", PluginID: "p"}).Validate(); err == nil {
		t.Fatal("缺 namespace 的 scope 必须拒绝")
	}
}

func TestPluginManagedCredentialRefsFailClosedAcrossPlugins(t *testing.T) {
	provider, err := kernelspi.NewPluginMapManagedCredentialRefs(map[string]map[string]pluginconfig.ManagedCredentialRef{
		"plugin.a": {"token": {Handle: "credential://managed/a", Scope: "tenant", Owner: "plugin.a", Purpose: "example.token", Version: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := provider.LookupManagedCredential(context.Background(), "plugin.b", "token"); err != nil || ok {
		t.Fatalf("plugin.b 不得读取 plugin.a 托管凭证: ok=%v err=%v", ok, err)
	}
	if ref, ok, err := provider.LookupManagedCredential(context.Background(), "plugin.a", "token"); err != nil || !ok || ref.Owner != "plugin.a" {
		t.Fatalf("plugin.a 应读取自己的托管凭证: ref=%+v ok=%v err=%v", ref, ok, err)
	}
}

func TestPluginMapConfigFailsClosedAcrossPlugins(t *testing.T) {
	provider, err := kernelspi.NewPluginMapConfig(map[string]map[string]any{
		"plugin.a": {"tokenRef": "credential://a"},
		"plugin.b": {"region": "cn-east"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := provider.Lookup(context.Background(), "plugin.a", "region"); err != nil || ok {
		t.Fatalf("plugin.a 不得读取 plugin.b 配置: ok=%v err=%v", ok, err)
	}
	if raw, ok, err := provider.Lookup(context.Background(), "plugin.b", "region"); err != nil || !ok || string(raw) != `"cn-east"` {
		t.Fatalf("plugin.b 应读取自己的配置: raw=%s ok=%v err=%v", raw, ok, err)
	}
}

func TestMemoryPersistenceTransactionIsolationRollbackAndConflict(t *testing.T) {
	ctx := context.Background()
	scope := kernelspi.Scope{TenantID: "t", PluginID: "p", Namespace: "state"}
	store := kernelspi.NewMemoryPersistence()
	if err := store.Put(ctx, scope, "key", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	tx, err := store.Begin(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Put(ctx, scope, "key", []byte("candidate")); err != nil {
		t.Fatal(err)
	}
	value, _ := store.Get(ctx, scope, "key")
	if string(value) != "v1" {
		t.Fatal("未提交事务不得污染当前视图")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	value, _ = store.Get(ctx, scope, "key")
	if string(value) != "v1" {
		t.Fatal("rollback 必须保留旧视图")
	}
	a, _ := store.Begin(ctx, scope)
	b, _ := store.Begin(ctx, scope)
	_ = a.Put(ctx, scope, "key", []byte("a"))
	_ = b.Put(ctx, scope, "key", []byte("b"))
	if err := a.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.Commit(ctx); !errors.Is(err, kernelspi.ErrTransactionConflict) {
		t.Fatalf("并发事务必须检测冲突: %v", err)
	}
}
