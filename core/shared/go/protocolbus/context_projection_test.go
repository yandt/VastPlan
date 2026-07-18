package protocolbus

import (
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/callcontext"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
)

func TestProjectContextForPluginAppliesManifestAndExtensionPointCeilings(t *testing.T) {
	wire := &contractv1.CallContext{
		TenantId: "acme", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "u1"},
		Principal:   &contractv1.Principal{UserId: "u1", Username: "secret-name", IsAdmin: true, SystemRoles: []string{"admin"}},
		Credentials: []*contractv1.CredentialRef{{Name: "db"}},
	}
	projected, err := projectContextForPlugin(wire, &contractv1.CallTarget{ExtensionPoint: extpoint.PermissionChecker}, LaunchPolicy{
		PluginID: "policy", ContextAccess: pluginv1.ContextAccess{
			Required: []string{string(callcontext.FieldCaller), string(callcontext.FieldAuthorizationRole)},
			Optional: []string{string(callcontext.FieldSubjectProfile), string(callcontext.FieldGrantCredentials)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(projected.GetPrincipal().GetSystemRoles()) != 1 {
		t.Fatal("权限策略应获得已申请角色")
	}
	if projected.GetPrincipal().GetUsername() != "" || len(projected.Credentials) != 0 {
		t.Fatalf("扩展点上限未裁剪 profile/grant: %+v", projected)
	}
}

func TestProjectContextForPluginFailsWhenPublisherCeilingRemovesRequiredField(t *testing.T) {
	_, err := projectContextForPlugin(&contractv1.CallContext{}, &contractv1.CallTarget{ExtensionPoint: extpoint.PermissionChecker}, LaunchPolicy{
		PluginID: "policy", ContextAccess: pluginv1.ContextAccess{Required: []string{string(callcontext.FieldAuthorizationRole)}},
		ContextCeiling: []string{string(callcontext.FieldScopeTenant)},
	})
	if err == nil {
		t.Fatal("发布者上限移除清单必需字段时必须 fail-closed")
	}
}

func TestUndeclaredContextDoesNotExpandToSensitiveFirstPartyFields(t *testing.T) {
	projected, err := projectContextForPlugin(&contractv1.CallContext{
		Principal:   &contractv1.Principal{Username: "hidden", IsAdmin: true},
		Credentials: []*contractv1.CredentialRef{{Name: "db"}},
	}, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage}, LaunchPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if projected.GetPrincipal().GetUsername() != "" || projected.GetPrincipal().GetIsAdmin() || len(projected.Credentials) != 0 {
		t.Fatalf("缺少 contextAccess 时泄露敏感字段: %+v", projected)
	}
}
