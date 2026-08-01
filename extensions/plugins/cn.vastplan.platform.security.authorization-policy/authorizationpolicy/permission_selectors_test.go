package authorizationpolicy

import (
	"reflect"
	"strings"
	"testing"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestMatchPermissionGlob(t *testing.T) {
	tests := []struct {
		name       string
		pattern    string
		permission string
		want       bool
	}{
		{name: "single segment", pattern: "platform.portal.*", permission: "platform.portal.read", want: true},
		{name: "single segment does not cross levels", pattern: "platform.portal.*", permission: "platform.portal.release.publish", want: false},
		{name: "recursive segments", pattern: "platform.**", permission: "platform.portal.release.publish", want: true},
		{name: "recursive requires one segment", pattern: "platform.**", permission: "platform", want: false},
		{name: "literal namespace", pattern: "platform.**", permission: "foundation.portal.read", want: false},
		{name: "middle wildcard", pattern: "platform.*.read", permission: "platform.portal.read", want: true},
		{name: "middle wildcard does not cross levels", pattern: "platform.*.read", permission: "platform.portal.draft.read", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := matchPermissionGlob(test.pattern, test.permission); got != test.want {
				t.Fatalf("matchPermissionGlob(%q, %q)=%v want=%v", test.pattern, test.permission, got, test.want)
			}
		})
	}
}

func TestResolvePermissionSelectorsCompilesEligibleExactCodes(t *testing.T) {
	eligible := map[string]pluginv1.PermissionCatalogEntry{
		"platform.portal.read":            {},
		"platform.portal.release.publish": {},
		"platform.database.read":          {},
	}
	resolved, sources, err := resolvePermissionSelectors([]PermissionSelector{
		{Kind: PermissionSelectorGlob, Value: "platform.portal.**"},
		{Kind: PermissionSelectorExact, Value: "platform.portal.read"},
	}, eligible)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"platform.portal.read", "platform.portal.release.publish"}
	if !reflect.DeepEqual(resolved, want) {
		t.Fatalf("Glob 必须稳定展开并去重: got=%v want=%v", resolved, want)
	}
	if len(sources) != 2 || sources[0].Kind != PermissionSelectorGlob || sources[1].Kind != PermissionSelectorExact {
		t.Fatalf("必须保留原始选择器用于审计: %+v", sources)
	}
}

func TestPermissionSelectorValidationFailsClosed(t *testing.T) {
	eligible := map[string]pluginv1.PermissionCatalogEntry{"platform.portal.read": {}}
	tests := []struct {
		name     string
		selector PermissionSelector
		contains string
	}{
		{name: "unknown exact", selector: PermissionSelector{Kind: PermissionSelectorExact, Value: "platform.portal.write"}, contains: "没有匹配"},
		{name: "leading wildcard", selector: PermissionSelector{Kind: PermissionSelectorGlob, Value: "**.read"}, contains: "字面命名空间"},
		{name: "partial wildcard", selector: PermissionSelector{Kind: PermissionSelectorGlob, Value: "platform.port*.read"}, contains: "无效"},
		{name: "regex", selector: PermissionSelector{Kind: PermissionSelectorGlob, Value: "platform.(portal|database).read"}, contains: "无效"},
		{name: "glob without wildcard", selector: PermissionSelector{Kind: PermissionSelectorGlob, Value: "platform.portal.read"}, contains: "必须包含"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := resolvePermissionSelectors([]PermissionSelector{test.selector}, eligible)
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("非法选择器必须被拒绝: err=%v", err)
			}
		})
	}
}

func TestCompileRoleStatementsRespectsDomainAndCatalogCeilings(t *testing.T) {
	catalog := pluginv1.PermissionCatalog{Permissions: []pluginv1.PermissionCatalogEntry{
		{PermissionDeclaration: pluginv1.PermissionDeclaration{Code: "platform.portal.read", Risk: "low", Assignable: true}},
		{PermissionDeclaration: pluginv1.PermissionDeclaration{Code: "platform.portal.internal", Risk: "low", Assignable: false}},
		{PermissionDeclaration: pluginv1.PermissionDeclaration{Code: "platform.portal.delete", Risk: "critical", Assignable: true}},
	}}
	domains := []authorizationv1.PolicyDomain{{
		ID: "platform.root",
		Delegation: authorizationv1.DelegationCeiling{
			Permissions: []string{"platform.portal.read", "platform.portal.internal", "platform.portal.delete"},
			MaxRisk:     authorizationv1.RiskHigh,
		},
	}}
	statements, _, err := compileRoleStatements("platform.reader", "platform.root", []RoleStatementInput{{
		ID: "allow", Effect: authorizationv1.EffectAllow,
		PermissionSelectors: []PermissionSelector{{Kind: PermissionSelectorGlob, Value: "platform.portal.*"}},
	}}, domains, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := statements[0].Permissions, []string{"platform.portal.read"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("选择器不得越过 assignable、Domain 或风险上限: got=%v want=%v", got, want)
	}
}

func TestRoleRevisionKeepsCompiledPermissionsAfterCatalogGrowth(t *testing.T) {
	catalog := pluginv1.PermissionCatalog{Digest: "catalog-a", Permissions: []pluginv1.PermissionCatalogEntry{
		{PermissionDeclaration: pluginv1.PermissionDeclaration{Code: "platform.portal.read", Risk: "low", Assignable: true}},
	}}
	domain := authorizationv1.PolicyDomain{ID: "platform.root", Delegation: authorizationv1.DelegationCeiling{Permissions: []string{"platform.portal.read"}, MaxRisk: authorizationv1.RiskCritical}}
	role, err := materializeRoleRevision(RoleRevision{ID: "platform.reader", DomainID: domain.ID}, []RoleStatementInput{{
		ID: "allow", Effect: authorizationv1.EffectAllow,
		PermissionSelectors: []PermissionSelector{{Kind: PermissionSelectorGlob, Value: "platform.**"}},
	}}, State{Catalog: catalog, Domains: []authorizationv1.PolicyDomain{domain}})
	if err != nil {
		t.Fatal(err)
	}
	catalog.Permissions = append(catalog.Permissions, pluginv1.PermissionCatalogEntry{PermissionDeclaration: pluginv1.PermissionDeclaration{Code: "platform.portal.delete", Risk: "critical", Assignable: true}})
	if got, want := role.Statements[0].Permissions, []string{"platform.portal.read"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Catalog 新增权限不得静默改变旧 Role revision: got=%v want=%v", got, want)
	}
	if role.SelectorCatalogDigest != "catalog-a" {
		t.Fatalf("Role revision 必须记录编译时 Catalog digest: %q", role.SelectorCatalogDigest)
	}
}
