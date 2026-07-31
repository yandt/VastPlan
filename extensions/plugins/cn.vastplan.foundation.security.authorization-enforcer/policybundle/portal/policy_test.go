package portal

import (
	"context"
	"encoding/json"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
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

func TestPortalUsersDeferToSignedCatalogAndSystemBreakGlass(t *testing.T) {
	user := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "author"}, Principal: &contractv1.Principal{SystemRoles: []string{"portal.compose"}}}
	for _, operation := range []string{"createPortal", "savePortalWorkingCopy", "submitPortalPublication", "approvePortalPublication", "publishPortalPublication", "releasePortalPublication"} {
		if got := decisionFor(t, user, portalapi.ComposerCapability, operation); got.Decision != extpoint.DecisionAbstain {
			t.Fatalf("用户操作 %s 必须交给签名 Permission Catalog: %+v", operation, got)
		}
	}
	system := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "system"}}
	if got := decisionFor(t, system, portalapi.ComposerCapability, "publishPortalPublication"); got.Decision != extpoint.DecisionAllow {
		t.Fatalf("system break-glass 应放行: %+v", got)
	}
}

func TestPortalPreferenceOnlyAllowsPortalBFFUser(t *testing.T) {
	user := &contractv1.CallContext{
		Scene:     "portal.bff",
		Caller:    &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "alice"},
		Principal: &contractv1.Principal{UserId: "alice"},
	}
	for _, operation := range []string{"get", "put"} {
		if got := decisionFor(t, user, portalapi.PreferenceCapability, operation); got.Decision != extpoint.DecisionAllow {
			t.Fatalf("Portal BFF preference %s should be allowed: %+v", operation, got)
		}
	}
	wrongScene := &contractv1.CallContext{
		Scene:     "plugin.call",
		Caller:    &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "alice"},
		Principal: &contractv1.Principal{UserId: "alice"},
	}
	if got := decisionFor(t, wrongScene, portalapi.PreferenceCapability, "get"); got.Decision != extpoint.DecisionDeny {
		t.Fatalf("non-BFF preference call must be denied: %+v", got)
	}
	if got := decisionFor(t, user, portalapi.PreferenceCapability, "deleteAll"); got.Decision != extpoint.DecisionDeny {
		t.Fatalf("unknown preference operation must be denied: %+v", got)
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
	if got := decisionFor(t, composer, portalapi.KernelArtifactReferencePublicationCapability, "publish"); got.Decision != extpoint.DecisionAllow {
		t.Fatalf("Composer 引用发布回调应放行: %+v", got)
	}
	if got := decisionFor(t, composer, portalapi.KernelTestArtifactValidationCapability, "validate"); got.Decision != extpoint.DecisionAllow {
		t.Fatalf("Composer 测试制品验证回调应放行: %+v", got)
	}
	for _, capability := range []string{"kernel.state.shared.get", "kernel.state.shared.create", "kernel.state.shared.update"} {
		if got := decisionFor(t, composer, capability, ""); got.Decision != extpoint.DecisionAllow {
			t.Fatalf("Composer Shared State 回调 %s 应放行: %+v", capability, got)
		}
	}
	if got := decisionFor(t, composer, workspacev1.Capability, workspacev1.OperationCommit); got.Decision != extpoint.DecisionAllow {
		t.Fatalf("Composer VersionControl 端口调用应放行: %+v", got)
	}
	other := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "evil"}}
	if got := decisionFor(t, other, portalapi.KernelCatalogValidationCapability, "validate"); got.Decision != extpoint.DecisionAbstain {
		t.Fatalf("非 Composer 必须不获授权: %+v", got)
	}
	if got := decisionFor(t, other, "kernel.state.shared.get", ""); got.Decision != extpoint.DecisionAbstain {
		t.Fatalf("非 Composer 不得继承 Shared State 授权: %+v", got)
	}
	if got := decisionFor(t, other, workspacev1.Capability, workspacev1.OperationCommit); got.Decision != extpoint.DecisionAbstain {
		t.Fatalf("非 Composer 不得继承 VersionControl 端口授权: %+v", got)
	}
}
