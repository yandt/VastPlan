package controlplane

import "testing"

func TestKeysAreStableAndDoNotLeakHierarchy(t *testing.T) {
	if got, want := DesiredKey("acme.cn", "prod/main"), "tenants.YWNtZS5jbg.states.cHJvZC9tYWlu"; got != want {
		t.Fatalf("DesiredKey=%q want=%q", got, want)
	}
	if got, want := DesiredKey("", "local"), "tenants.X2dsb2JhbA.states.bG9jYWw"; got != want {
		t.Fatalf("global DesiredKey=%q want=%q", got, want)
	}
	if ActualKey("node.1") != NodeKey("node.1") {
		t.Fatal("actual 与 lease 的节点 key token 应一致")
	}
	if got, want := AssignmentKey("acme", "prod", "node.1"), "tenants.YWNtZQ.states.cHJvZA.nodes.bm9kZS4x"; got != want {
		t.Fatalf("AssignmentKey=%q want=%q", got, want)
	}
}
