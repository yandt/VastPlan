package policy

import (
	"context"
	"encoding/json"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

func decisionFor(t *testing.T, ctx *contractv1.CallContext, capability, operation string) extpoint.PermissionResponse {
	t.Helper()
	raw, _ := json.Marshal(extpoint.PermissionRequest{Capability: capability, Operation: operation})
	_, out, err := Check(context.Background(), ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	var result extpoint.PermissionResponse
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPortalRolesAndSystemBreakGlass(t *testing.T) {
	user := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "author"}, Principal: &contractv1.Principal{SystemRoles: []string{"portal.compose"}}}
	if got := decisionFor(t, user, portalapi.ComposerCapability, "createDraft"); got.Decision != extpoint.DecisionAllow {
		t.Fatalf("compose 应放行: %+v", got)
	}
	if got := decisionFor(t, user, portalapi.ComposerCapability, "updateDraft"); got.Decision != extpoint.DecisionAllow {
		t.Fatalf("compose 应允许更新草稿: %+v", got)
	}
	if got := decisionFor(t, user, portalapi.ComposerCapability, "publish"); got.Decision != extpoint.DecisionDeny {
		t.Fatalf("publish 应拒绝: %+v", got)
	}
	system := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "system"}}
	if got := decisionFor(t, system, portalapi.ComposerCapability, "publish"); got.Decision != extpoint.DecisionAllow {
		t.Fatalf("system break-glass 应放行: %+v", got)
	}
}

func TestOnlyComposerCanUseRestrictedKernelServices(t *testing.T) {
	composer := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: PluginIDForComposer()}}
	if got := decisionFor(t, composer, portalapi.KernelCatalogValidationCapability, "validate"); got.Decision != extpoint.DecisionAllow {
		t.Fatalf("Composer catalog 回调应放行: %+v", got)
	}
	if got := decisionFor(t, composer, portalapi.KernelCatalogMaterializationCapability, "materialize"); got.Decision != extpoint.DecisionAllow {
		t.Fatalf("Composer materialize 回调应放行: %+v", got)
	}
	other := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "evil"}}
	if got := decisionFor(t, other, portalapi.KernelCatalogValidationCapability, "validate"); got.Decision != extpoint.DecisionAbstain {
		t.Fatalf("非 Composer 必须不获授权: %+v", got)
	}
}
