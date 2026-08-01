package authorizationpolicy

import (
	"fmt"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type BootstrapGrant struct {
	RoleID              string
	Title               string
	SubjectID           string
	PermissionSelectors []PermissionSelector
}

func BuildBootstrapState(catalog pluginv1.PermissionCatalog, profile authorizationv1.ProviderProfile, domains []authorizationv1.PolicyDomain, grants []BootstrapGrant, now time.Time) (State, error) {
	if len(grants) == 0 {
		return State{}, fmt.Errorf("Authorization Bootstrap 至少需要一个安全管理员")
	}
	state := State{Version: stateVersion, Generation: 1, PolicyRevision: 1, Catalog: catalog, ProviderProfile: profile, Domains: append([]authorizationv1.PolicyDomain(nil), domains...), Roles: []RoleRevision{}, Bindings: []BindingRevision{}, Revocations: []authorizationv1.Revocation{}, Audit: []AuditEvent{}}
	now = now.UTC()
	for _, grant := range grants {
		role := RoleRevision{ID: grant.RoleID, Revision: 1, DomainID: rootDomainID(domains), Title: grant.Title, State: StatePublished, CreatedBy: "seed-authority", ApprovedBy: "trusted-host", CreatedAt: now, UpdatedAt: now}
		statements, selectors, err := compileRoleStatements(role.ID, role.DomainID, []RoleStatementInput{{ID: "bootstrap-allow", Effect: authorizationv1.EffectAllow, PermissionSelectors: grant.PermissionSelectors, Constraints: []authorizationv1.AttributeConstraint{}}}, domains, catalog)
		if err != nil {
			return State{}, err
		}
		role.SelectorCatalogDigest, role.PermissionSelectors, role.Statements = catalog.Digest, selectors, statements
		state.Roles = append(state.Roles, role)
		state.Bindings = append(state.Bindings, BindingRevision{ID: grant.RoleID + ".binding", Revision: 1, DomainID: role.DomainID, Subject: authorizationv1.Subject{Kind: authorizationv1.SubjectUser, ID: grant.SubjectID, Issuer: authenticationv1.StableSubjectIssuer}, RoleID: role.ID, RoleRevision: role.Revision, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(24 * time.Hour), State: StatePublished, CreatedBy: "seed-authority", ApprovedBy: "trusted-host", CreatedAt: now, UpdatedAt: now})
	}
	if len(state.Bindings) == 0 {
		return State{}, fmt.Errorf("Authorization Bootstrap 没有产生有效绑定")
	}
	return state, nil
}
