package controlplane

import (
	"strings"
	"testing"
)

func TestKeysAreStableAndDoNotLeakHierarchy(t *testing.T) {
	if got, want := DesiredKey("acme.cn", "prod/main"), "tenants.YWNtZS5jbg.states.cHJvZC9tYWlu"; got != want {
		t.Fatalf("DesiredKey=%q want=%q", got, want)
	}
	if got, want := DesiredKey("", "local"), "tenants.X2dsb2JhbA.states.bG9jYWw"; got != want {
		t.Fatalf("global DesiredKey=%q want=%q", got, want)
	}
	if got, want := ActualKey("acme", "prod", "node.1"), "tenants.YWNtZQ.states.cHJvZA.actual.bm9kZS4x"; got != want {
		t.Fatalf("ActualKey=%q want=%q", got, want)
	}
	if ActualKey("acme", "prod", "node.1") == ActualKey("acme", "other", "node.1") {
		t.Fatal("实际态必须绑定 tenant/deployment 作用域")
	}
	if got, want := AssignmentKey("acme", "prod", "node.1"), "tenants.YWNtZQ.states.cHJvZA.nodes.bm9kZS4x"; got != want {
		t.Fatalf("AssignmentKey=%q want=%q", got, want)
	}
}

func TestRPCSubjectForAllowsOneOptionalRoutingDimension(t *testing.T) {
	for _, subject := range []string{
		RPCSubjectFor("platform.database", "platform.database", ""),
		RPCSubjectFor("platform.database", "", "core"),
	} {
		if strings.HasSuffix(subject, ".") || strings.Contains(subject, "..") {
			t.Fatalf("NATS subject 不得包含空 token: %q", subject)
		}
	}
}

func TestRPCInstanceSubjectIsScopedAndOpaque(t *testing.T) {
	first := RPCInstanceSubject("foundation.database", "database-runtime", "platform", "", "runtime:v1:secret-looking")
	second := RPCInstanceSubject("foundation.database", "database-runtime", "platform", "", "runtime:v1:other")
	if first == second || !strings.Contains(first, ".instance.") {
		t.Fatalf("实例 subject 必须唯一且位于共享路由下: %q %q", first, second)
	}
	if strings.Contains(first, "runtime:v1") || strings.Contains(first, "secret-looking") {
		t.Fatalf("实例 ID 必须编码后进入 NATS subject: %q", first)
	}
}
