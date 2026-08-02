package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	policy "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.authorization-policy/authorizationpolicy"
)

func TestReconcileDevelopmentGrantsOnlyChangesSeedOwnedRoles(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	catalog := developmentAuthorizationCatalog(t)
	profile := policy.NativeProviderProfile(catalog)
	root, err := policy.RootDomain(catalog, profile)
	if err != nil {
		t.Fatal(err)
	}
	state, err := policy.BuildBootstrapState(catalog, profile, []authorizationv1.PolicyDomain{root}, developmentGrants(catalog, ""), now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	state.Roles = append(state.Roles, policy.RoleRevision{
		ID: "custom", Revision: 1, CreatedBy: "local-admin",
		Statements: []authorizationv1.PolicyStatement{{Permissions: []string{"platform.removed"}}},
	})
	if err := reconcileDevelopmentGrants(&state, developmentGrants(catalog, ""), now); err != nil {
		t.Fatal(err)
	}
	if got := state.Roles[0].Statements[0].Permissions; len(got) != 1 || got[0] != "platform.database.read" {
		t.Fatalf("Seed owner 权限未按当前目录收敛: %v", got)
	}
	if got := state.Roles[len(state.Roles)-1].Statements[0].Permissions; len(got) != 1 || got[0] != "platform.removed" {
		t.Fatalf("用户角色不得被开发 Seed 静默改写: %v", got)
	}
}

func TestReconcileDevelopmentGrantsAddsSeedOwnerAfterInitialization(t *testing.T) {
	now := time.Date(2026, 8, 1, 6, 0, 0, 0, time.UTC)
	catalog := developmentAuthorizationCatalog(t)
	profile := policy.NativeProviderProfile(catalog)
	root, err := policy.RootDomain(catalog, profile)
	if err != nil {
		t.Fatal(err)
	}
	state, err := policy.BuildBootstrapState(catalog, profile, []authorizationv1.PolicyDomain{root}, developmentGrants(catalog, ""), now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	const subjectID = "seed-user-1"
	if err := reconcileDevelopmentGrants(&state, developmentGrants(catalog, subjectID), now); err != nil {
		t.Fatal(err)
	}
	var foundRole, foundBinding bool
	for _, role := range state.Roles {
		if role.ID == "platform.seed-owner" {
			foundRole = len(role.PermissionSelectors) == 1 && role.PermissionSelectors[0].Selectors[0].Value == "platform.**" && equalStrings(role.Statements[0].Permissions, []string{"platform.database.read"})
		}
	}
	for _, binding := range state.Bindings {
		if binding.ID == "platform.seed-owner.binding" {
			foundBinding = binding.Subject.ID == subjectID && binding.Subject.Issuer == authenticationv1.StableSubjectIssuer && binding.State == policy.StatePublished
		}
	}
	if !foundRole || !foundBinding {
		t.Fatalf("显式 bootstrap 未补齐 Seed Owner: role=%v binding=%v", foundRole, foundBinding)
	}
}

func TestReconcileDevelopmentGrantsRejectsUntrustedReservedRole(t *testing.T) {
	now := time.Date(2026, 8, 1, 6, 0, 0, 0, time.UTC)
	catalog := developmentAuthorizationCatalog(t)
	profile := policy.NativeProviderProfile(catalog)
	root, err := policy.RootDomain(catalog, profile)
	if err != nil {
		t.Fatal(err)
	}
	state, err := policy.BuildBootstrapState(catalog, profile, []authorizationv1.PolicyDomain{root}, developmentGrants(catalog, ""), now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	state.Roles = append(state.Roles, policy.RoleRevision{ID: "platform.seed-owner", Revision: 1, CreatedBy: "local-admin"})
	if err := reconcileDevelopmentGrants(&state, developmentGrants(catalog, "seed-user-1"), now); err == nil || !strings.Contains(err.Error(), "非受信定义占用") {
		t.Fatalf("保留 Role 冲突必须 fail closed: %v", err)
	}
}

func TestReconcileDevelopmentGrantsBeforeCatalogUpdateUsesTargetCatalog(t *testing.T) {
	now := time.Date(2026, 8, 1, 6, 0, 0, 0, time.UTC)
	oldCatalog := developmentAuthorizationCatalog(t)
	oldCatalog.Permissions = append(oldCatalog.Permissions, pluginv1.PermissionCatalogEntry{
		PermissionDeclaration: pluginv1.PermissionDeclaration{Code: "platform.removed.read", Title: "Removed", Scope: "platform", Risk: "low", Assignable: true},
		PluginID:              "cn.vastplan.test.removed", PluginVersion: "1.0.0", Publisher: "vastplan", ArtifactSHA256: strings.Repeat("b", 64),
	})
	digest, err := pluginv1.PermissionCatalogDigest(oldCatalog)
	if err != nil {
		t.Fatal(err)
	}
	oldCatalog.Digest = digest
	profile := policy.NativeProviderProfile(oldCatalog)
	root, err := policy.RootDomain(oldCatalog, profile)
	if err != nil {
		t.Fatal(err)
	}
	state, err := policy.BuildBootstrapState(oldCatalog, profile, []authorizationv1.PolicyDomain{root}, developmentGrants(oldCatalog, ""), now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	store := &policy.FileStore{Path: filepath.Join(t.TempDir(), "policy-state.json")}
	if _, err := store.CompareAndSwap(0, state); err != nil {
		t.Fatal(err)
	}
	targetCatalog := developmentAuthorizationCatalog(t)
	if err := reconcileDevelopmentGrantsBeforeCatalogUpdate(store, targetCatalog, developmentGrants(targetCatalog, ""), now); err != nil {
		t.Fatal(err)
	}
	reconciled, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.Catalog.Digest != oldCatalog.Digest {
		t.Fatal("预收敛不得绕过正式 Catalog 迁移")
	}
	if got := reconciled.Roles[0].Statements[0].Permissions; !equalStrings(got, []string{"platform.database.read"}) {
		t.Fatalf("Seed Role 必须按目标 Catalog 收敛: %v", got)
	}
}

func TestDevelopmentGrantsKeepPortalLifecycleSeparated(t *testing.T) {
	catalog := pluginv1.PermissionCatalog{Permissions: []pluginv1.PermissionCatalogEntry{
		{PermissionDeclaration: pluginv1.PermissionDeclaration{Code: "platform.portal.read", Assignable: true}},
		{PermissionDeclaration: pluginv1.PermissionDeclaration{Code: "platform.portal.compose", Assignable: true}},
		{PermissionDeclaration: pluginv1.PermissionDeclaration{Code: "platform.portal.approve", Assignable: true}},
		{PermissionDeclaration: pluginv1.PermissionDeclaration{Code: "platform.portal.publish", Assignable: true}},
	}}
	grants := developmentGrants(catalog, "")
	if len(grants) != 4 {
		t.Fatalf("开发引导角色数量错误: %d", len(grants))
	}
	want := map[string][]string{
		"local-author":    {"platform.portal.read", "platform.portal.compose"},
		"local-approver":  {"platform.portal.read", "platform.portal.approve"},
		"local-publisher": {"platform.portal.read", "platform.portal.publish"},
	}
	for _, grant := range grants[1:] {
		got := make([]string, 0, len(grant.PermissionSelectors))
		for _, selector := range grant.PermissionSelectors {
			got = append(got, selector.Value)
		}
		if !equalStrings(got, want[grant.SubjectID]) {
			t.Fatalf("主体 %s 的 Portal 权限未与签名目录同步: got=%v want=%v", grant.SubjectID, got, want[grant.SubjectID])
		}
	}
}

func developmentAuthorizationCatalog(t *testing.T) pluginv1.PermissionCatalog {
	t.Helper()
	const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	catalog := pluginv1.PermissionCatalog{
		SchemaVersion: pluginv1.PermissionCatalogSchemaVersion,
		Permissions: []pluginv1.PermissionCatalogEntry{{
			PermissionDeclaration: pluginv1.PermissionDeclaration{Code: "platform.database.read", Title: "Read database connections", Scope: "platform", Risk: "high", Assignable: true},
			PluginID:              "cn.vastplan.platform.data.relational.connection-manager", PluginVersion: "1.0.0", Publisher: "vastplan", ArtifactSHA256: digest,
		}},
		Operations: []pluginv1.PermissionOperationEntry{{
			OperationGuard: pluginv1.OperationGuard{ExtensionPoint: "tool.package", Capability: "platform.database", Operation: "list", Permissions: []string{"platform.database.read"}, Access: "read", Approval: "none"},
			PluginID:       "cn.vastplan.platform.data.relational.connection-manager", PluginVersion: "1.0.0", ArtifactSHA256: digest,
		}},
	}
	computed, err := pluginv1.PermissionCatalogDigest(catalog)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Digest = computed
	return catalog
}
