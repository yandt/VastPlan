package profile

import (
	"context"
	"testing"
)

type catalog struct{}

func (catalog) SupportsDesktop(context.Context, PluginRef) (bool, error) { return true, nil }
func TestValidateAndEligibility(t *testing.T) {
	p := Profile{Version: 1, ID: "collector", Revision: 1, TenantID: "tenant-a", Runtime: "desktop", Distribution: "self-update", Targets: []string{"darwin/arm64"}, AssignedTo: []string{"desktop-a"}, Plugins: []PluginRef{{ID: "cn.vastplan.collector", Version: "1.0.0"}}}
	if err := Validate(context.Background(), p, catalog{}); err != nil {
		t.Fatal(err)
	}
	if !Eligible(p, "desktop-a") || Eligible(p, "desktop-b") {
		t.Fatal("领取约束错误")
	}
}

func TestClaimLaunchBindsVerifiedIdentityToTenantAndAssignment(t *testing.T) {
	p := Profile{Version: 1, ID: "collector", Revision: 1, TenantID: "tenant-a", AssignedTo: []string{"desktop-a"}, Plugins: []PluginRef{{ID: "x", Version: "1"}}}
	claim, err := ClaimLaunch(context.Background(), DesktopIdentity{ID: "desktop-a", TenantID: "tenant-a"}, p)
	if err != nil || claim.DesktopID != "desktop-a" || claim.TenantID != "tenant-a" {
		t.Fatalf("领取应绑定身份: %+v %v", claim, err)
	}
	if _, err := ClaimLaunch(context.Background(), DesktopIdentity{ID: "desktop-a", TenantID: "tenant-b"}, p); err == nil {
		t.Fatal("跨 tenant 领取必须拒绝")
	}
	if _, err := ClaimLaunch(context.Background(), DesktopIdentity{ID: "desktop-b", TenantID: "tenant-a"}, p); err == nil {
		t.Fatal("未分配 Desktop 必须拒绝")
	}
}
