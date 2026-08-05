package sharedstate

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestBindingStoreOnlyMovesForwardAndNeverFallsBack(t *testing.T) {
	binding := NewBindingStore()
	scope := Scope{Kind: ScopeService, RuntimeScope: "service-a", PluginID: "cn.vastplan.test", Namespace: "state"}
	if _, err := binding.Get(context.Background(), scope, "key"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("未绑定时必须 fail-closed: %v", err)
	}
	first, _ := OpenFileStore(filepath.Join(t.TempDir(), "first.json"))
	if err := binding.Bind(1, "profile-a", first); err != nil {
		t.Fatal(err)
	}
	if _, err := binding.Create(context.Background(), scope, "key", []byte("value")); err != nil {
		t.Fatal(err)
	}
	second, _ := OpenFileStore(filepath.Join(t.TempDir(), "second.json"))
	if err := binding.Bind(1, "profile-b", second); !errors.Is(err, ErrConflict) {
		t.Fatalf("同 generation 身份漂移必须拒绝: %v", err)
	}
	if err := binding.Bind(0, "profile-old", second); !errors.Is(err, ErrInvalid) {
		t.Fatalf("无效 generation 必须拒绝: %v", err)
	}
	if err := binding.Bind(2, "profile-b", second); err != nil {
		t.Fatal(err)
	}
	if _, err := binding.Get(context.Background(), scope, "key"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("新代必须使用新 Provider，不能回退旧 Store: %v", err)
	}
}

func TestBindingStoreSignalsOnlySuccessfulGenerationSwitches(t *testing.T) {
	binding := NewBindingStore()
	first, _ := OpenFileStore(filepath.Join(t.TempDir(), "first.json"))
	if err := binding.Bind(1, "profile-a", first); err != nil {
		t.Fatal(err)
	}
	select {
	case <-binding.Changes():
	default:
		t.Fatal("first generation did not signal")
	}
	if err := binding.Bind(1, "profile-a", first); err != nil {
		t.Fatal(err)
	}
	select {
	case <-binding.Changes():
		t.Fatal("idempotent bind must not signal")
	default:
	}
	second, _ := OpenFileStore(filepath.Join(t.TempDir(), "second.json"))
	if err := binding.Bind(2, "profile-b", second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-binding.Changes():
	default:
		t.Fatal("new generation did not signal")
	}
}
