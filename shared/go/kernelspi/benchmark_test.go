package kernelspi

import (
	"context"
	"testing"
)

func BenchmarkBackend_PersistenceGet(b *testing.B) {
	ctx := context.Background()
	scope := Scope{TenantID: "t", PluginID: "p", Namespace: "n"}
	store := NewMemoryPersistence()
	_ = store.Put(ctx, scope, "key", []byte("value"))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.Get(ctx, scope, "key"); err != nil {
			b.Fatal(err)
		}
	}
}
