package authorizationpolicy

import (
	"fmt"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type BootstrapGrant struct {
	RoleID      string
	Title       string
	SubjectID   string
	Permissions []string
}

func BuildBootstrapState(catalog pluginv1.PermissionCatalog, profile authorizationv1.ProviderProfile, domains []authorizationv1.PolicyDomain, grants []BootstrapGrant, now time.Time) (State, error) {
	if len(grants) == 0 {
		return State{}, fmt.Errorf("Authorization Bootstrap 至少需要一个安全管理员")
	}
	state := State{Version: stateVersion, Generation: 1, PolicyRevision: 1, Catalog: catalog, ProviderProfile: profile, Domains: append([]authorizationv1.PolicyDomain(nil), domains...), Roles: []RoleRevision{}, Bindings: []BindingRevision{}, Revocations: []authorizationv1.Revocation{}, Audit: []AuditEvent{}}
	known := catalogPermissions(catalog)
	now = now.UTC()
	for _, grant := range grants {
		permissions := []string{}
		for _, code := range grant.Permissions {
			entry, exists := known[code]
			if exists && entry.Assignable {
				permissions = append(permissions, code)
			}
		}
		if len(permissions) == 0 {
			continue
		}
		role := RoleRevision{ID: grant.RoleID, Revision: 1, DomainID: rootDomainID(domains), Title: grant.Title, Statements: []authorizationv1.PolicyStatement{{ID: "bootstrap-allow", Effect: authorizationv1.EffectAllow, Permissions: permissions, Constraints: []authorizationv1.AttributeConstraint{}}}, State: StatePublished, CreatedBy: "seed-authority", ApprovedBy: "trusted-host", CreatedAt: now, UpdatedAt: now}
		if err := validateRole(role, domains, known); err != nil {
			return State{}, err
		}
		state.Roles = append(state.Roles, role)
		state.Bindings = append(state.Bindings, BindingRevision{ID: grant.RoleID + ".binding", Revision: 1, DomainID: role.DomainID, Subject: authorizationv1.Subject{Kind: authorizationv1.SubjectUser, ID: grant.SubjectID, Issuer: authenticationv1.StableSubjectIssuer}, RoleID: role.ID, RoleRevision: role.Revision, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(24 * time.Hour), State: StatePublished, CreatedBy: "seed-authority", ApprovedBy: "trusted-host", CreatedAt: now, UpdatedAt: now})
	}
	if len(state.Bindings) == 0 {
		return State{}, fmt.Errorf("Authorization Bootstrap 没有产生有效绑定")
	}
	return state, nil
}
