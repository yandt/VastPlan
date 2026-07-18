package bootstrappolicy

import (
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
)

func TestBootstrapPolicyProtectsSettingsWrites(t *testing.T) {
	write := extpoint.PermissionRequest{Capability: SettingsCapability, Operation: "put"}
	read := extpoint.PermissionRequest{Capability: SettingsCapability, Operation: "get"}
	if got := evaluateWriteGuard(systemContext(), write); got.Decision != extpoint.DecisionAbstain {
		t.Fatalf("system 写入应交给基线或动态策略: %+v", got)
	}
	if got := evaluateWriteGuard(adminUserContext(), write); got.Decision != extpoint.DecisionAbstain {
		t.Fatalf("管理员用户写入应交给后续策略: %+v", got)
	}
	if got := evaluateWriteGuard(pluginContext("com.vastplan.platform.security.credentials", true), write); got.Decision != extpoint.DecisionDeny {
		t.Fatalf("插件不能继承管理员 principal 写权限: %+v", got)
	}
	if got := evaluateWriteGuard(pluginContext("com.vastplan.foundation.security.reader", false), read); got.Decision != extpoint.DecisionAbstain {
		t.Fatalf("只读操作应交给基线: %+v", got)
	}
	if got := evaluateWriteGuard(pluginContext("com.vastplan.foundation.security.reader", false), extpoint.PermissionRequest{
		Capability: SettingsCapability, Operation: "futureOperation",
	}); got.Decision != extpoint.DecisionDeny {
		t.Fatalf("未知操作必须按写操作保护: %+v", got)
	}
}

func TestBootstrapBaselineUsesVerifiedNamespaceClassification(t *testing.T) {
	read := extpoint.PermissionRequest{Capability: SettingsCapability, Operation: "list"}
	write := extpoint.PermissionRequest{Capability: SettingsCapability, Operation: "delete"}
	for _, id := range []string{"com.vastplan.foundation.security.credentials-bootstrap", "com.vastplan.platform.data.database"} {
		if got := evaluateBaseline(pluginContext(id, false), read); got.Decision != extpoint.DecisionAllow {
			t.Fatalf("基础层首方插件 %s 应允许只读: %+v", id, got)
		}
	}
	for _, id := range []string{"com.vastplan.product.agent.studio", "com.vastplan.example.security.demo", "com.example.foundation.security.fake"} {
		if got := evaluateBaseline(pluginContext(id, false), read); got.Decision != extpoint.DecisionDeny {
			t.Fatalf("非自举层插件 %s 必须默认拒绝: %+v", id, got)
		}
	}
	if got := evaluateBaseline(systemContext(), write); got.Decision != extpoint.DecisionAllow {
		t.Fatalf("system 应允许写入: %+v", got)
	}
	if got := evaluateBaseline(adminUserContext(), write); got.Decision != extpoint.DecisionAllow {
		t.Fatalf("直接登录管理员应允许写入: %+v", got)
	}
}

func TestBootstrapPolicyAbstainsOutsideSettingsCapability(t *testing.T) {
	request := extpoint.PermissionRequest{Capability: "product.agent.run", Operation: "invoke"}
	if got := evaluateWriteGuard(systemContext(), request); got.Decision != extpoint.DecisionAbstain {
		t.Fatalf("写保护不应接管其他能力: %+v", got)
	}
	if got := evaluateBaseline(systemContext(), request); got.Decision != extpoint.DecisionAbstain {
		t.Fatalf("基线不应接管其他能力: %+v", got)
	}
}

func systemContext() *contractv1.CallContext {
	return &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "kernel"}}
}

func adminUserContext() *contractv1.CallContext {
	return &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "admin"}, Principal: &contractv1.Principal{IsAdmin: true}}
}

func pluginContext(id string, inheritedAdmin bool) *contractv1.CallContext {
	return &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: id}, Principal: &contractv1.Principal{IsAdmin: inheritedAdmin}}
}
