package authorizationpolicy

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

const (
	maximumRoleStatements          = 64
	maximumSelectorsPerStatement   = 128
	maximumResolvedPermissions     = 512
	maximumPermissionSelectorBytes = 200
)

var permissionSegment = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func compileRoleStatements(roleID, domainID string, inputs []RoleStatementInput, domains []authorizationv1.PolicyDomain, catalog pluginv1.PermissionCatalog) ([]authorizationv1.PolicyStatement, []StatementPermissionSelectors, error) {
	if len(inputs) == 0 || len(inputs) > maximumRoleStatements {
		return nil, nil, errors.New("Role Statements 数量无效")
	}
	domain := findDomain(domains, domainID)
	if domain == nil {
		return nil, nil, fmt.Errorf("Role %s 引用未知 Domain %s", roleID, domainID)
	}
	eligible := eligibleRolePermissions(*domain, catalog)
	statementIDs := make(map[string]struct{}, len(inputs))
	statements := make([]authorizationv1.PolicyStatement, 0, len(inputs))
	sources := make([]StatementPermissionSelectors, 0, len(inputs))
	for _, input := range inputs {
		if input.ID == "" {
			return nil, nil, errors.New("Role Statement ID 不能为空")
		}
		if _, duplicate := statementIDs[input.ID]; duplicate {
			return nil, nil, fmt.Errorf("Role Statement %s 重复", input.ID)
		}
		statementIDs[input.ID] = struct{}{}
		if input.Effect != authorizationv1.EffectAllow && input.Effect != authorizationv1.EffectDeny {
			return nil, nil, fmt.Errorf("Role Statement %s effect 无效", input.ID)
		}
		resolved, selectors, err := resolvePermissionSelectors(input.PermissionSelectors, eligible)
		if err != nil {
			return nil, nil, fmt.Errorf("Role %s Statement %s: %w", roleID, input.ID, err)
		}
		statements = append(statements, authorizationv1.PolicyStatement{
			ID: input.ID, Effect: input.Effect, Permissions: resolved,
			Resource: cloneResourceSelector(input.Resource), Constraints: cloneConstraints(input.Constraints),
		})
		sources = append(sources, StatementPermissionSelectors{StatementID: input.ID, Selectors: selectors})
	}
	return statements, sources, nil
}

func eligibleRolePermissions(domain authorizationv1.PolicyDomain, catalog pluginv1.PermissionCatalog) map[string]pluginv1.PermissionCatalogEntry {
	ceiling := make(map[string]struct{}, len(domain.Delegation.Permissions))
	for _, permission := range domain.Delegation.Permissions {
		ceiling[permission] = struct{}{}
	}
	eligible := map[string]pluginv1.PermissionCatalogEntry{}
	for _, permission := range catalog.Permissions {
		if _, allowed := ceiling[permission.Code]; permission.Assignable && allowed && riskRank(permission.Risk) <= riskRank(string(domain.Delegation.MaxRisk)) {
			eligible[permission.Code] = permission
		}
	}
	return eligible
}

func resolvePermissionSelectors(values []PermissionSelector, eligible map[string]pluginv1.PermissionCatalogEntry) ([]string, []PermissionSelector, error) {
	if len(values) == 0 || len(values) > maximumSelectorsPerStatement {
		return nil, nil, errors.New("权限选择器数量无效")
	}
	seenSelectors := map[string]struct{}{}
	resolved := map[string]struct{}{}
	selectors := make([]PermissionSelector, 0, len(values))
	codes := make([]string, 0, len(eligible))
	for code := range eligible {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, selector := range values {
		if err := validatePermissionSelector(selector); err != nil {
			return nil, nil, err
		}
		key := string(selector.Kind) + "\x00" + selector.Value
		if _, duplicate := seenSelectors[key]; duplicate {
			return nil, nil, fmt.Errorf("权限选择器重复: %s", selector.Value)
		}
		seenSelectors[key] = struct{}{}
		matched := 0
		for _, code := range codes {
			if selector.Kind == PermissionSelectorExact && code != selector.Value {
				continue
			}
			if selector.Kind == PermissionSelectorGlob && !matchPermissionGlob(selector.Value, code) {
				continue
			}
			resolved[code] = struct{}{}
			matched++
		}
		if matched == 0 {
			return nil, nil, fmt.Errorf("权限选择器没有匹配当前 Catalog 与 Domain: %s", selector.Value)
		}
		selectors = append(selectors, selector)
	}
	if len(resolved) > maximumResolvedPermissions {
		return nil, nil, fmt.Errorf("权限选择器展开超过 %d 项", maximumResolvedPermissions)
	}
	result := make([]string, 0, len(resolved))
	for code := range resolved {
		result = append(result, code)
	}
	sort.Strings(result)
	return result, selectors, nil
}

func validatePermissionSelector(selector PermissionSelector) error {
	if selector.Value == "" || len(selector.Value) > maximumPermissionSelectorBytes {
		return errors.New("权限选择器长度无效")
	}
	segments := strings.Split(selector.Value, ".")
	if len(segments) < 2 || !permissionSegment.MatchString(segments[0]) {
		return fmt.Errorf("权限选择器必须以字面命名空间开头: %s", selector.Value)
	}
	switch selector.Kind {
	case PermissionSelectorExact:
		for _, segment := range segments {
			if !permissionSegment.MatchString(segment) {
				return fmt.Errorf("精确权限选择器无效: %s", selector.Value)
			}
		}
	case PermissionSelectorGlob:
		hasWildcard := false
		for _, segment := range segments {
			if segment == "*" || segment == "**" {
				hasWildcard = true
				continue
			}
			if !permissionSegment.MatchString(segment) {
				return fmt.Errorf("Glob 权限选择器无效: %s", selector.Value)
			}
		}
		if !hasWildcard {
			return fmt.Errorf("Glob 权限选择器必须包含 * 或 **: %s", selector.Value)
		}
	default:
		return fmt.Errorf("未知权限选择器类型 %s", selector.Kind)
	}
	return nil
}

func matchPermissionGlob(pattern, permission string) bool {
	patterns := strings.Split(pattern, ".")
	segments := strings.Split(permission, ".")
	type position struct{ pattern, permission int }
	memo := map[position]bool{}
	visited := map[position]bool{}
	var match func(int, int) bool
	match = func(patternIndex, permissionIndex int) bool {
		key := position{patternIndex, permissionIndex}
		if visited[key] {
			return memo[key]
		}
		visited[key] = true
		if patternIndex == len(patterns) {
			memo[key] = permissionIndex == len(segments)
			return memo[key]
		}
		if patterns[patternIndex] == "**" {
			for next := permissionIndex + 1; next <= len(segments); next++ {
				if match(patternIndex+1, next) {
					memo[key] = true
					return true
				}
			}
			return false
		}
		if permissionIndex >= len(segments) || patterns[patternIndex] != "*" && patterns[patternIndex] != segments[permissionIndex] {
			return false
		}
		memo[key] = match(patternIndex+1, permissionIndex+1)
		return memo[key]
	}
	return match(0, 0)
}

func cloneResourceSelector(value *authorizationv1.ResourceSelector) *authorizationv1.ResourceSelector {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.IDs = append([]string(nil), value.IDs...)
	cloned.Labels = make(map[string][]string, len(value.Labels))
	for key, values := range value.Labels {
		cloned.Labels[key] = append([]string(nil), values...)
	}
	return &cloned
}

func cloneConstraints(values []authorizationv1.AttributeConstraint) []authorizationv1.AttributeConstraint {
	result := make([]authorizationv1.AttributeConstraint, len(values))
	copy(result, values)
	for index := range result {
		result[index].Values = append([]string(nil), result[index].Values...)
	}
	return result
}

func exactPermissionSelectors(values []string) []PermissionSelector {
	selectors := make([]PermissionSelector, 0, len(values))
	for _, value := range values {
		selectors = append(selectors, PermissionSelector{Kind: PermissionSelectorExact, Value: value})
	}
	return selectors
}
