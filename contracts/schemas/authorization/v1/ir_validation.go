package authorizationv1

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

func ParseAuthorizationIR(raw []byte) (AuthorizationIR, error) {
	if len(raw) > MaxAuthorizationIRBytes {
		return AuthorizationIR{}, fmt.Errorf("Authorization IR 超过 %d bytes", MaxAuthorizationIRBytes)
	}
	if err := validateSchema(IRSchemaURL, raw); err != nil {
		return AuthorizationIR{}, err
	}
	var policy AuthorizationIR
	if err := decodeStrict(raw, &policy); err != nil {
		return AuthorizationIR{}, err
	}
	if err := validateAuthorizationIRSemantics(policy); err != nil {
		return AuthorizationIR{}, err
	}
	return policy, nil
}

func ValidateAuthorizationIR(policy AuthorizationIR) error {
	raw, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	if err := validateSchema(IRSchemaURL, raw); err != nil {
		return err
	}
	return validateAuthorizationIRSemantics(policy)
}

func validateAuthorizationIRSemantics(policy AuthorizationIR) error {
	profiles := make(map[string]ProviderProfile, len(policy.ProviderProfiles))
	for _, profile := range policy.ProviderProfiles {
		if _, exists := profiles[profile.ID]; exists {
			return fmt.Errorf("Provider Profile 重复: %s", profile.ID)
		}
		if err := ValidateProviderProfile(profile); err != nil {
			return fmt.Errorf("Provider Profile %s 无效: %w", profile.ID, err)
		}
		profiles[profile.ID] = profile
	}
	domains := make(map[string]PolicyDomain, len(policy.Domains))
	for _, domain := range policy.Domains {
		if _, exists := domains[domain.ID]; exists {
			return fmt.Errorf("Policy Domain 重复: %s", domain.ID)
		}
		if _, exists := profiles[domain.ProviderProfileID]; !exists {
			return fmt.Errorf("Policy Domain %s 引用未知 Provider Profile %s", domain.ID, domain.ProviderProfileID)
		}
		if hasDuplicates(domain.Delegation.Permissions) {
			return fmt.Errorf("Policy Domain %s 的委托权限重复", domain.ID)
		}
		if domain.Delegation.OfflineAllowed && riskRank(domain.Delegation.MaxRisk) >= riskRank(RiskHigh) {
			return fmt.Errorf("Policy Domain %s 的高风险委托不得离线", domain.ID)
		}
		domains[domain.ID] = domain
	}
	root, exists := domains[policy.RootDomainID]
	if !exists || root.Kind != DomainPlatform || root.ParentID != "" {
		return errors.New("rootDomainId 必须指向无父级的 platform Domain")
	}
	rootCount := 0
	for _, domain := range policy.Domains {
		if domain.Kind == DomainPlatform && domain.ParentID == "" {
			rootCount++
		}
		if err := validateDomainScope(domain); err != nil {
			return err
		}
		if domain.ID == policy.RootDomainID {
			continue
		}
		parent, parentExists := domains[domain.ParentID]
		if !parentExists {
			return fmt.Errorf("Policy Domain %s 缺少父级 %s", domain.ID, domain.ParentID)
		}
		if err := validateDomainDelegation(parent, domain); err != nil {
			return err
		}
	}
	if rootCount != 1 {
		return fmt.Errorf("必须且只能有一个 platform 根 Domain，实际 %d", rootCount)
	}
	if err := validateDomainCycles(domains, policy.RootDomainID); err != nil {
		return err
	}
	roles := make(map[string]CompiledRole, len(policy.Roles))
	for _, role := range policy.Roles {
		key := role.ID + "@" + fmt.Sprint(role.Revision)
		if _, exists := roles[key]; exists {
			return fmt.Errorf("Role revision 重复: %s", key)
		}
		domain, exists := domains[role.DomainID]
		if !exists {
			return fmt.Errorf("Role %s 引用未知 Domain %s", role.ID, role.DomainID)
		}
		ceiling := stringSet(domain.Delegation.Permissions)
		statementIDs := map[string]struct{}{}
		for _, statement := range role.Statements {
			if _, duplicate := statementIDs[statement.ID]; duplicate {
				return fmt.Errorf("Role %s 的 statement 重复: %s", role.ID, statement.ID)
			}
			statementIDs[statement.ID] = struct{}{}
			for _, permission := range statement.Permissions {
				if _, allowed := ceiling[permission]; !allowed {
					return fmt.Errorf("Role %s 权限 %s 超出 Domain %s 委托上限", role.ID, permission, role.DomainID)
				}
			}
		}
		roles[key] = role
	}
	bindingIDs := map[string]struct{}{}
	for _, binding := range policy.Bindings {
		if _, duplicate := bindingIDs[binding.ID]; duplicate {
			return fmt.Errorf("Subject Binding 重复: %s", binding.ID)
		}
		bindingIDs[binding.ID] = struct{}{}
		role, exists := roles[binding.RoleID+"@"+fmt.Sprint(binding.RoleRevision)]
		if !exists || role.DomainID != binding.DomainID {
			return fmt.Errorf("Binding %s 必须引用同 Domain 的精确 Role revision", binding.ID)
		}
		if binding.NotBefore.IsZero() || !binding.ExpiresAt.After(binding.NotBefore) {
			return fmt.Errorf("Binding %s 的有效时间窗无效", binding.ID)
		}
	}
	var lastRevocation uint64
	seenRevocations := map[string]struct{}{}
	ordered := append([]Revocation(nil), policy.Revocations...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Revision < ordered[j].Revision })
	for _, revocation := range ordered {
		if _, duplicate := seenRevocations[revocation.ID]; duplicate {
			return fmt.Errorf("Revocation 重复: %s", revocation.ID)
		}
		seenRevocations[revocation.ID] = struct{}{}
		if revocation.Revision <= lastRevocation || revocation.Revision > policy.RevocationRevision {
			return fmt.Errorf("Revocation %s 的 revision 不单调或越界", revocation.ID)
		}
		lastRevocation = revocation.Revision
	}
	return nil
}

func validateDomainScope(domain PolicyDomain) error {
	scope := domain.Scope
	switch domain.Kind {
	case DomainPlatform:
		if scope.TenantID != "" || scope.ProjectID != "" || scope.ResourceType != "" || scope.ResourceID != "" {
			return fmt.Errorf("platform Domain %s 不得伪造下级 scope", domain.ID)
		}
	case DomainTenant:
		if scope.TenantID == "" || scope.ProjectID != "" || scope.ResourceType != "" || scope.ResourceID != "" {
			return fmt.Errorf("tenant Domain %s scope 无效", domain.ID)
		}
	case DomainProject:
		if scope.TenantID == "" || scope.ProjectID == "" || scope.ResourceType != "" || scope.ResourceID != "" {
			return fmt.Errorf("project Domain %s scope 无效", domain.ID)
		}
	case DomainResource:
		if scope.TenantID == "" || scope.ResourceType == "" || scope.ResourceID == "" {
			return fmt.Errorf("resource Domain %s scope 无效", domain.ID)
		}
	}
	return nil
}

func validateDomainDelegation(parent, child PolicyDomain) error {
	allowedTransition := (parent.Kind == DomainPlatform && child.Kind == DomainTenant) ||
		(parent.Kind == DomainTenant && (child.Kind == DomainProject || child.Kind == DomainResource)) ||
		(parent.Kind == DomainProject && child.Kind == DomainResource)
	if !allowedTransition {
		return fmt.Errorf("不允许的 Policy Domain 层级 %s -> %s", parent.Kind, child.Kind)
	}
	if !parent.Delegation.MayDelegate {
		return fmt.Errorf("父 Domain %s 禁止继续委托", parent.ID)
	}
	if child.Scope.TenantID != parent.Scope.TenantID && parent.Kind != DomainPlatform {
		return fmt.Errorf("子 Domain %s tenant 必须继承父级", child.ID)
	}
	if parent.Kind == DomainProject && child.Scope.ProjectID != parent.Scope.ProjectID {
		return fmt.Errorf("资源 Domain %s project 必须继承父级", child.ID)
	}
	parentPermissions := stringSet(parent.Delegation.Permissions)
	for _, permission := range child.Delegation.Permissions {
		if _, exists := parentPermissions[permission]; !exists {
			return fmt.Errorf("子 Domain %s 权限 %s 超出父级委托上限", child.ID, permission)
		}
	}
	if riskRank(child.Delegation.MaxRisk) > riskRank(parent.Delegation.MaxRisk) || child.Delegation.MaxTTLSeconds > parent.Delegation.MaxTTLSeconds {
		return fmt.Errorf("子 Domain %s 扩大了父级风险或 TTL 上限", child.ID)
	}
	if child.Delegation.OfflineAllowed && !parent.Delegation.OfflineAllowed {
		return fmt.Errorf("子 Domain %s 扩大了父级离线授权能力", child.ID)
	}
	return nil
}

func validateDomainCycles(domains map[string]PolicyDomain, rootID string) error {
	for id := range domains {
		seen := map[string]struct{}{}
		current := id
		for current != "" && current != rootID {
			if _, exists := seen[current]; exists {
				return fmt.Errorf("Policy Domain 存在循环: %s", id)
			}
			seen[current] = struct{}{}
			current = domains[current].ParentID
		}
		if current != rootID {
			return fmt.Errorf("Policy Domain %s 未连接根 Domain", id)
		}
	}
	return nil
}

func riskRank(risk Risk) int {
	return map[Risk]int{RiskLow: 1, RiskMedium: 2, RiskHigh: 3, RiskCritical: 4}[risk]
}
func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}
func hasDuplicates(values []string) bool { return len(stringSet(values)) != len(values) }
