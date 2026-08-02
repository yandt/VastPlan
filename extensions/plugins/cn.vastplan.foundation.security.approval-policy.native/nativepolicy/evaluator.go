package nativepolicy

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
)

type factValue struct {
	scalar string
	list   []string
	exists bool
}

func evaluate(profile compiledProfile, input approvalv2.EvaluationInput) (approvalv2.Decision, error) {
	for _, rule := range profile.profile.Rules {
		matched, err := matchesAll(rule.Conditions, input)
		if err != nil {
			return approvalv2.Decision{}, err
		}
		if !matched {
			continue
		}
		switch rule.Effect {
		case approvalv2.EffectAllow:
			return decision(profile.ref, rule, approvalv2.DecisionAllowed, rule.Code, rule.Message, nil, auditNote(profile.ref, rule.ID, nil)), nil
		case approvalv2.EffectDeny:
			code := rule.Code
			if code == "" {
				code = "approval.policy.denied"
			}
			return decision(profile.ref, rule, approvalv2.DecisionDenied, code, rule.Message, nil, ""), nil
		case approvalv2.EffectRequireEvidence:
			status, code, message, auditFields, err := evaluateEvidence(rule.Requirements, input)
			if err != nil {
				return approvalv2.Decision{}, err
			}
			audit := ""
			if status == approvalv2.DecisionAllowed {
				audit = auditNote(profile.ref, rule.ID, auditFields)
			}
			return decision(profile.ref, rule, status, code, message, rule.Requirements, audit), nil
		}
	}
	return approvalv2.Decision{Status: approvalv2.DecisionDenied, Profile: profile.ref, Code: "approval.policy.default_deny", Message: "当前审批策略没有允许该操作"}, nil
}

func decision(ref approvalv2.ProfileRef, rule approvalv2.Rule, status approvalv2.DecisionStatus, code, message string, requirements []approvalv2.EvidenceRequirement, audit string) approvalv2.Decision {
	return approvalv2.Decision{Status: status, Profile: ref, MatchedRuleID: rule.ID, Code: code, Message: message, Requirements: append([]approvalv2.EvidenceRequirement(nil), requirements...), AuditNote: audit}
}

func matchesAll(conditions []approvalv2.Condition, input approvalv2.EvaluationInput) (bool, error) {
	for _, condition := range conditions {
		matched, err := matchCondition(condition, input)
		if err != nil || !matched {
			return matched, err
		}
	}
	return true, nil
}

func matchCondition(condition approvalv2.Condition, input approvalv2.EvaluationInput) (bool, error) {
	left, err := resolveFact(condition.Left, input)
	if err != nil {
		return false, err
	}
	if condition.Operator == approvalv2.OperatorExists {
		return left.exists, nil
	}
	if condition.Operator == approvalv2.OperatorNotExists {
		return !left.exists, nil
	}
	right := factValue{scalar: condition.Value, exists: condition.Value != ""}
	if condition.RightFact != "" {
		right, err = resolveFact(condition.RightFact, input)
		if err != nil {
			return false, err
		}
	}
	switch condition.Operator {
	case approvalv2.OperatorEquals, approvalv2.OperatorNotEquals:
		if len(left.list) != 0 || len(right.list) != 0 {
			return false, errors.New("equals 只能比较标量事实")
		}
		matched := left.exists && right.exists && left.scalar == right.scalar
		if condition.Operator == approvalv2.OperatorNotEquals {
			matched = left.exists && right.exists && left.scalar != right.scalar
		}
		return matched, nil
	case approvalv2.OperatorContains, approvalv2.OperatorNotContains:
		if len(left.list) == 0 || !right.exists || len(right.list) != 0 {
			return false, errors.New("contains 要求左侧为列表、右侧为标量")
		}
		matched := false
		for _, item := range left.list {
			if item == right.scalar {
				matched = true
				break
			}
		}
		if condition.Operator == approvalv2.OperatorNotContains {
			matched = !matched
		}
		return matched, nil
	default:
		return false, fmt.Errorf("未知 Approval operator %s", condition.Operator)
	}
}

func resolveFact(ref string, input approvalv2.EvaluationInput) (factValue, error) {
	switch ref {
	case "operation":
		return scalarFact(input.Operation), nil
	case "actor.id":
		return scalarFact(input.Actor.ID), nil
	case "actor.roles":
		return factValue{list: append([]string(nil), input.Actor.Roles...), exists: len(input.Actor.Roles) != 0}, nil
	case "resource.id":
		return scalarFact(input.Resource.ID), nil
	case "resource.digest":
		return scalarFact(input.Resource.Digest), nil
	case "resource.submittedBy":
		return scalarFact(input.Resource.SubmittedBy), nil
	}
	if key, ok := strings.CutPrefix(ref, "resource.attributes."); ok {
		return scalarFact(input.Resource.Attributes[key]), nil
	}
	if key, ok := strings.CutPrefix(ref, "context."); ok {
		return scalarFact(input.Context[key]), nil
	}
	return factValue{}, fmt.Errorf("未知 Approval fact %s", ref)
}

func scalarFact(value string) factValue {
	value = strings.TrimSpace(value)
	return factValue{scalar: value, exists: value != ""}
}

func auditNote(ref approvalv2.ProfileRef, ruleID string, fields map[string]string) string {
	parts := []string{fmt.Sprintf("policy=%s@%d", ref.ID, ref.Revision), "digest=" + ref.Digest, "rule=" + ruleID}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, key+"="+strings.ReplaceAll(fields[key], "\n", " "))
	}
	return strings.Join(parts, " ")
}
