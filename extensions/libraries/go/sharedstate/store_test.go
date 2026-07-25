package sharedstate

import "testing"

func TestScopeRejectsCallerControlledIdentityGaps(t *testing.T) {
	valid := Scope{Kind: ScopeTenant, TenantID: "tenant-a", PluginID: "cn.vastplan.demo", RuntimeScope: "service-a", Namespace: "settings.values"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := []Scope{
		{Kind: ScopeTenant, PluginID: valid.PluginID, RuntimeScope: valid.RuntimeScope, Namespace: valid.Namespace},
		{Kind: ScopeService, TenantID: "forged", PluginID: valid.PluginID, RuntimeScope: valid.RuntimeScope, Namespace: valid.Namespace},
		{Kind: ScopeTenant, TenantID: valid.TenantID, PluginID: "", RuntimeScope: valid.RuntimeScope, Namespace: valid.Namespace},
		{Kind: ScopeTenant, TenantID: valid.TenantID, PluginID: valid.PluginID, RuntimeScope: valid.RuntimeScope, Namespace: "../escape"},
	}
	for _, scope := range invalid {
		if err := scope.Validate(); err == nil {
			t.Fatalf("必须拒绝非法 scope: %+v", scope)
		}
	}
}

func TestValueAndListLimits(t *testing.T) {
	if ValidateValue(make([]byte, MaxValueBytes)) != nil || ValidateValue(make([]byte, MaxValueBytes+1)) == nil {
		t.Fatal("shared state value 上限错误")
	}
	if ValidateList("settings.", MaxPageSize, "settings.a") != nil || ValidateList("", MaxPageSize+1, "") == nil {
		t.Fatal("shared state list 上限错误")
	}
}
