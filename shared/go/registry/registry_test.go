package registry

import "testing"

func newWithPoints() *Registry {
	r := New()
	r.DefinePoint(ExtensionPoint{Name: "tool.package", Dispatch: DispatchSingle})
	r.DefinePoint(ExtensionPoint{Name: "event.sink", Dispatch: DispatchFanout})
	return r
}

// 未定义的扩展点必须拒绝（fail-closed）。
func TestRegister_UndefinedPointRejected(t *testing.T) {
	r := newWithPoints()
	err := r.Register(Contribution{ExtensionPoint: "no.such.point", ID: "x", PluginID: "p1"})
	if err == nil {
		t.Fatal("未定义扩展点应被拒绝，实际通过了")
	}
}

// single 语义下同一能力 id 不得被两个插件重复提供。
func TestRegister_SingleDispatchRejectsDuplicate(t *testing.T) {
	r := newWithPoints()
	if err := r.Register(Contribution{ExtensionPoint: "tool.package", ID: "a.b", PluginID: "p1"}); err != nil {
		t.Fatalf("首次注册应成功: %v", err)
	}
	err := r.Register(Contribution{ExtensionPoint: "tool.package", ID: "a.b", PluginID: "p2"})
	if err == nil {
		t.Fatal("single 语义下重复 id 应被拒绝，实际通过了")
	}
}

// fanout 语义下多个插件可提供同扩展点的不同贡献，且按 priority 降序返回。
func TestList_FanoutOrderedByPriority(t *testing.T) {
	r := newWithPoints()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("注册失败: %v", err)
		}
	}
	must(r.Register(Contribution{ExtensionPoint: "event.sink", ID: "low", PluginID: "p1", Priority: 1}))
	must(r.Register(Contribution{ExtensionPoint: "event.sink", ID: "high", PluginID: "p2", Priority: 10}))

	got := r.List("event.sink")
	if len(got) != 2 {
		t.Fatalf("期望 2 条贡献，实际 %d", len(got))
	}
	if got[0].ID != "high" || got[1].ID != "low" {
		t.Fatalf("期望按 priority 降序 [high low]，实际 [%s %s]", got[0].ID, got[1].ID)
	}
}

// Lookup 是 single 语义的解析路径。
func TestLookup(t *testing.T) {
	r := newWithPoints()
	_ = r.Register(Contribution{ExtensionPoint: "tool.package", ID: "a.b", PluginID: "p1"})

	if c, ok := r.Lookup("tool.package", "a.b"); !ok || c.PluginID != "p1" {
		t.Fatalf("应解析到 p1 提供的 a.b，实际 ok=%v c=%+v", ok, c)
	}
	if _, ok := r.Lookup("tool.package", "not.there"); ok {
		t.Fatal("未注册能力应解析失败")
	}
}

// 插件崩溃/停用时应摘除其全部贡献（ADR-0004 故障隔离）。
func TestUnregisterPlugin(t *testing.T) {
	r := newWithPoints()
	_ = r.Register(Contribution{ExtensionPoint: "tool.package", ID: "a.b", PluginID: "p1"})
	_ = r.Register(Contribution{ExtensionPoint: "event.sink", ID: "sink1", PluginID: "p1"})
	_ = r.Register(Contribution{ExtensionPoint: "event.sink", ID: "sink2", PluginID: "p2"})

	if n := r.UnregisterPlugin("p1"); n != 2 {
		t.Fatalf("期望摘除 p1 的 2 条贡献，实际 %d", n)
	}
	if _, ok := r.Lookup("tool.package", "a.b"); ok {
		t.Fatal("p1 的贡献应已被摘除")
	}
	if got := r.List("event.sink"); len(got) != 1 || got[0].PluginID != "p2" {
		t.Fatalf("p2 的贡献应保留，实际 %+v", got)
	}
}

// 解绑后同一 id 可被重新注册——支撑热装（ADR-0003）。
func TestReregisterAfterUnregister(t *testing.T) {
	r := newWithPoints()
	_ = r.Register(Contribution{ExtensionPoint: "tool.package", ID: "a.b", PluginID: "p1"})
	r.UnregisterPlugin("p1")

	if err := r.Register(Contribution{ExtensionPoint: "tool.package", ID: "a.b", PluginID: "p2"}); err != nil {
		t.Fatalf("解绑后应可重新注册（热装），实际: %v", err)
	}
}
