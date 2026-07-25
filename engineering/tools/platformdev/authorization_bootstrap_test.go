package main

import (
	"testing"
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	policy "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.authorization-policy/authorizationpolicy"
)

func TestReconcileDevelopmentGrantsOnlyChangesSeedOwnedRoles(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	catalog := pluginv1.PermissionCatalog{Permissions: []pluginv1.PermissionCatalogEntry{
		{PermissionDeclaration: pluginv1.PermissionDeclaration{Code: "platform.current", Assignable: true}},
	}}
	state := policy.State{Roles: []policy.RoleRevision{
		{
			ID: "platform.owner", Revision: 1, CreatedBy: "seed-authority",
			Statements: []authorizationv1.PolicyStatement{{Permissions: []string{"platform.removed"}}},
		},
		{
			ID: "custom", Revision: 1, CreatedBy: "local-admin",
			Statements: []authorizationv1.PolicyStatement{{Permissions: []string{"platform.removed"}}},
		},
	}}
	reconcileDevelopmentGrants(&state, catalog, now)
	if got := state.Roles[0].Statements[0].Permissions; len(got) != 1 || got[0] != "platform.current" {
		t.Fatalf("Seed owner 权限未按当前目录收敛: %v", got)
	}
	if got := state.Roles[1].Statements[0].Permissions; len(got) != 1 || got[0] != "platform.removed" {
		t.Fatalf("用户角色不得被开发 Seed 静默改写: %v", got)
	}
}
