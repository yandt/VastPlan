// Package authorizationnative is the shared first-party implementation of the
// bounded Authorization IR evaluator. Runtime plugins remain independent and
// only share this SDK implementation package at build time.
package authorizationnative

import (
	"fmt"
	"sort"
	"strings"
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
)

type Evaluation struct {
	Decision          authorizationv1.AuthorizationDecision
	ReasonCode        string
	MatchedRoleIDs    []string
	MatchedBindingIDs []string
}

func Evaluate(policy authorizationv1.AuthorizationIR, input authorizationv1.EvaluationInput, now time.Time) Evaluation {
	revoked := activeRevocations(policy.Revocations, now)
	if _, denied := revoked["subject\x00"+input.Subject.ID]; denied {
		return Evaluation{Decision: authorizationv1.DecisionDeny, ReasonCode: "authorization.subject_revoked"}
	}
	roles := make(map[string]authorizationv1.CompiledRole, len(policy.Roles))
	for _, role := range policy.Roles {
		roles[fmt.Sprintf("%s@%d", role.ID, role.Revision)] = role
	}
	groups := map[string]struct{}{}
	for _, group := range input.ExternalGroups {
		groups[group.Issuer+"\x00"+group.ID] = struct{}{}
	}
	allowed, denied := map[string]struct{}{}, map[string]struct{}{}
	matchedRoles, matchedBindings := map[string]struct{}{}, map[string]struct{}{}
	for _, binding := range policy.Bindings {
		if binding.DomainID != input.DomainID || now.Before(binding.NotBefore) || !now.Before(binding.ExpiresAt) {
			continue
		}
		if _, off := revoked["binding\x00"+binding.ID]; off {
			continue
		}
		if _, off := revoked["role\x00"+binding.RoleID]; off {
			continue
		}
		if !subjectMatches(binding.Subject, input.Subject, groups) {
			continue
		}
		role, exists := roles[fmt.Sprintf("%s@%d", binding.RoleID, binding.RoleRevision)]
		if !exists || role.DomainID != input.DomainID {
			continue
		}
		bindingMatched := false
		for _, statement := range role.Statements {
			if !resourceMatches(statement.Resource, input.Scope) || !constraintsMatch(statement.Constraints, input) {
				continue
			}
			target := allowed
			if statement.Effect == authorizationv1.EffectDeny {
				target = denied
			}
			for _, permission := range statement.Permissions {
				target[permission] = struct{}{}
			}
			bindingMatched = true
		}
		if bindingMatched {
			matchedRoles[role.ID] = struct{}{}
			matchedBindings[binding.ID] = struct{}{}
		}
	}
	for _, permission := range input.RequiredPermissions {
		if _, explicitDeny := denied[permission]; explicitDeny {
			return Evaluation{Decision: authorizationv1.DecisionDeny, ReasonCode: "authorization.explicit_deny", MatchedRoleIDs: sortedKeys(matchedRoles), MatchedBindingIDs: sortedKeys(matchedBindings)}
		}
		if _, ok := allowed[permission]; !ok {
			return Evaluation{Decision: authorizationv1.DecisionDeny, ReasonCode: "authorization.permission_missing", MatchedRoleIDs: sortedKeys(matchedRoles), MatchedBindingIDs: sortedKeys(matchedBindings)}
		}
	}
	return Evaluation{Decision: authorizationv1.DecisionAllow, ReasonCode: "authorization.allowed", MatchedRoleIDs: sortedKeys(matchedRoles), MatchedBindingIDs: sortedKeys(matchedBindings)}
}

func subjectMatches(binding, subject authorizationv1.Subject, groups map[string]struct{}) bool {
	if binding.Kind == authorizationv1.SubjectUser {
		return binding.ID == subject.ID && binding.Issuer == subject.Issuer
	}
	if binding.Kind == authorizationv1.SubjectGroup {
		_, exists := groups[binding.Issuer+"\x00"+binding.ID]
		return exists
	}
	return false
}

func resourceMatches(selector *authorizationv1.ResourceSelector, scope authorizationv1.EvaluationScope) bool {
	if selector == nil {
		return true
	}
	if selector.Type != scope.ResourceType {
		return false
	}
	if len(selector.IDs) > 0 && !contains(selector.IDs, scope.ResourceID) {
		return false
	}
	return selector.Ownership == "" || selector.Ownership == "any"
}

func constraintsMatch(constraints []authorizationv1.AttributeConstraint, input authorizationv1.EvaluationInput) bool {
	for _, constraint := range constraints {
		actual := constraintValue(constraint.Source, constraint.Key, input)
		switch constraint.Operator {
		case "eq", "in":
			if !contains(constraint.Values, actual) {
				return false
			}
		case "prefix":
			matched := false
			for _, prefix := range constraint.Values {
				if strings.HasPrefix(actual, prefix) {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func constraintValue(source, key string, input authorizationv1.EvaluationInput) string {
	switch source + "." + key {
	case "subject.id":
		return input.Subject.ID
	case "subject.issuer":
		return input.Subject.Issuer
	case "scope.tenant":
		return input.Scope.TenantID
	case "scope.project":
		return input.Scope.ProjectID
	case "scope.resourceType":
		return input.Scope.ResourceType
	case "scope.resourceId":
		return input.Scope.ResourceID
	case "target.capability":
		return input.Target.Capability
	case "target.operation":
		return input.Target.Operation
	default:
		return ""
	}
}

func activeRevocations(values []authorizationv1.Revocation, now time.Time) map[string]struct{} {
	result := map[string]struct{}{}
	for _, value := range values {
		if !now.Before(value.EffectiveAt) {
			result[value.Kind+"\x00"+value.TargetID] = struct{}{}
		}
	}
	return result
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
