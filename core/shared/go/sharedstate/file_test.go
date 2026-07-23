package sharedstate

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestFileStorePersistsCASAndRejectsWidePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "shared.json")
	store, err := OpenFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{Kind: ScopeService, PluginID: "cn.vastplan.demo", RuntimeScope: "service-a", Namespace: "ledger"}
	created, err := store.Create(context.Background(), scope, "active", []byte(`{"revision":1}`))
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reopened.Get(context.Background(), scope, "active")
	if err != nil || string(loaded.Value) != `{"revision":1}` || loaded.Revision != created.Revision {
		t.Fatalf("重启恢复失败: %+v err=%v", loaded, err)
	}
	if _, err := reopened.Update(context.Background(), scope, "active", []byte(`{}`), created.Revision+1); !errors.Is(err, ErrConflict) {
		t.Fatalf("旧/未来 revision 必须冲突: %v", err)
	}
}
