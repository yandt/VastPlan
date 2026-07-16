package registry

import "testing"

func BenchmarkBackend_RegistryLookup(b *testing.B) {
	r := New()
	r.DefinePoint(ExtensionPoint{Name: "tool.package", Dispatch: DispatchSingle})
	_ = r.Register(Contribution{ExtensionPoint: "tool.package", ID: "demo.echo", PluginID: "demo"})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := r.Lookup("tool.package", "demo.echo"); !ok {
			b.Fatal("missing")
		}
	}
}

func BenchmarkBackend_RegistryFanoutList64(b *testing.B) {
	r := New()
	r.DefinePoint(ExtensionPoint{Name: "event.sink", Dispatch: DispatchFanout})
	for i := 0; i < 64; i++ {
		_ = r.Register(Contribution{ExtensionPoint: "event.sink", ID: string(rune('a'+i)) + ".sink", PluginID: "demo", Priority: i})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if len(r.List("event.sink")) != 64 {
			b.Fatal("bad list")
		}
	}
}
