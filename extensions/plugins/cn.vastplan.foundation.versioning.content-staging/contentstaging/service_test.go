package contentstaging

import (
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
)

func TestControlPlaneOnlyAcceptsWorkspaceAndPreservesUserSubject(t *testing.T) {
	call := &contractv1.CallContext{
		TenantId: "tenant-a", Principal: &contractv1.Principal{UserId: "alice"},
		Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: workspacePluginID},
	}
	scope, err := requestScope(call)
	if err != nil || scope.ActorID != "user:alice" {
		t.Fatalf("scope: %+v %v", scope, err)
	}
	call.Caller.Id = "cn.vastplan.application.example"
	if _, err := requestScope(call); err == nil {
		t.Fatal("非 Workspace 插件不应取得控制面")
	}
}
