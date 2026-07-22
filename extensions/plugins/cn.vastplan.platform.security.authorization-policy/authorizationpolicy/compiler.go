package authorizationpolicy

import (
	"errors"
	"fmt"
	"sort"
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func CompileSnapshot(state State, audience []string, issuedAt time.Time, ttl time.Duration) (authorizationv1.PolicySnapshot, error) {
	if ttl <= 0 || ttl > 24*time.Hour {
		return authorizationv1.PolicySnapshot{}, errors.New("Policy Snapshot TTL 必须在 1 秒到 24 小时之间")
	}
	if len(audience) == 0 || len(audience) > 64 {
		return authorizationv1.PolicySnapshot{}, errors.New("Policy Snapshot audience 数量无效")
	}
	permissions := catalogPermissions(state.Catalog)
	roles := make([]authorizationv1.CompiledRole, 0, len(state.Roles))
	for _, role := range state.Roles {
		if role.State != StatePublished {
			continue
		}
		if err := validateRole(role, state.Domains, permissions); err != nil {
			return authorizationv1.PolicySnapshot{}, err
		}
		roles = append(roles, authorizationv1.CompiledRole{ID: role.ID, Revision: role.Revision, DomainID: role.DomainID, Statements: cloneStatements(role.Statements)})
	}
	bindings := make([]authorizationv1.SubjectBinding, 0, len(state.Bindings))
	for _, binding := range state.Bindings {
		if binding.State != StatePublished {
			continue
		}
		bindings = append(bindings, authorizationv1.SubjectBinding{ID: binding.ID, Revision: binding.Revision, DomainID: binding.DomainID, Subject: binding.Subject, RoleID: binding.RoleID, RoleRevision: binding.RoleRevision, NotBefore: binding.NotBefore.UTC(), ExpiresAt: binding.ExpiresAt.UTC()})
	}
	policy := authorizationv1.AuthorizationIR{
		SchemaVersion: authorizationv1.IRSchemaVersion, CatalogDigest: state.Catalog.Digest,
		RootDomainID: rootDomainID(state.Domains), ProviderProfiles: []authorizationv1.ProviderProfile{state.ProviderProfile},
		Domains: append([]authorizationv1.PolicyDomain(nil), state.Domains...), Roles: roles, Bindings: bindings,
		Revocations: append([]authorizationv1.Revocation{}, state.Revocations...), RevocationRevision: state.RevocationRevision,
	}
	if err := authorizationv1.ValidateAuthorizationIR(policy); err != nil {
		return authorizationv1.PolicySnapshot{}, fmt.Errorf("编译 Authorization IR: %w", err)
	}
	sort.Strings(audience)
	issuedAt = issuedAt.UTC()
	return authorizationv1.PolicySnapshot{
		SchemaVersion: authorizationv1.IRSchemaVersion, SnapshotID: fmt.Sprintf("platform.policy.%d", state.PolicyRevision), Revision: state.PolicyRevision,
		Audience: append([]string(nil), audience...), IssuedAt: issuedAt, NotBefore: issuedAt, ExpiresAt: issuedAt.Add(ttl), Policy: policy,
	}, nil
}

func validateRole(role RoleRevision, domains []authorizationv1.PolicyDomain, permissions map[string]pluginv1.PermissionCatalogEntry) error {
	domain := findDomain(domains, role.DomainID)
	if domain == nil {
		return fmt.Errorf("Role %s 引用未知 Domain %s", role.ID, role.DomainID)
	}
	ceiling := map[string]struct{}{}
	for _, code := range domain.Delegation.Permissions {
		ceiling[code] = struct{}{}
	}
	for _, statement := range role.Statements {
		for _, code := range statement.Permissions {
			entry, exists := permissions[code]
			if !exists || !entry.Assignable {
				return fmt.Errorf("Role %s 引用未知或不可分配权限 %s", role.ID, code)
			}
			if _, allowed := ceiling[code]; !allowed {
				return fmt.Errorf("Role %s 权限 %s 超出 Domain 委托上限", role.ID, code)
			}
			if riskRank(entry.Risk) > riskRank(string(domain.Delegation.MaxRisk)) {
				return fmt.Errorf("Role %s 权限 %s 风险超出 Domain 上限", role.ID, code)
			}
		}
	}
	return nil
}

func catalogPermissions(catalog pluginv1.PermissionCatalog) map[string]pluginv1.PermissionCatalogEntry {
	out := make(map[string]pluginv1.PermissionCatalogEntry, len(catalog.Permissions))
	for _, permission := range catalog.Permissions {
		out[permission.Code] = permission
	}
	return out
}

func rootDomainID(domains []authorizationv1.PolicyDomain) string {
	for _, domain := range domains {
		if domain.Kind == authorizationv1.DomainPlatform && domain.ParentID == "" {
			return domain.ID
		}
	}
	return ""
}

func findDomain(domains []authorizationv1.PolicyDomain, id string) *authorizationv1.PolicyDomain {
	for index := range domains {
		if domains[index].ID == id {
			return &domains[index]
		}
	}
	return nil
}

func cloneStatements(values []authorizationv1.PolicyStatement) []authorizationv1.PolicyStatement {
	out := make([]authorizationv1.PolicyStatement, len(values))
	copy(out, values)
	for index := range out {
		out[index].Permissions = append([]string{}, out[index].Permissions...)
		out[index].Constraints = append([]authorizationv1.AttributeConstraint{}, out[index].Constraints...)
	}
	return out
}

func riskRank(value string) int {
	return map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}[value]
}
