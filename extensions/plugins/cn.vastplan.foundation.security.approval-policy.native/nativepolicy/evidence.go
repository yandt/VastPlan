package nativepolicy

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
)

func evaluateEvidence(requirements []approvalv2.EvidenceRequirement, input approvalv2.EvaluationInput) (approvalv2.DecisionStatus, string, string, map[string]string, error) {
	missing := false
	audit := map[string]string{}
	for _, requirement := range requirements {
		raw, exists := input.Evidence[requirement.Field]
		if !exists {
			missing = true
			continue
		}
		valid, printable, err := validateEvidence(requirement, raw, input)
		if err != nil {
			return approvalv2.DecisionDenied, "approval.evidence.invalid", "审批证据格式无效", nil, nil
		}
		if !valid {
			return approvalv2.DecisionDenied, "approval.evidence.mismatch", "审批证据与当前冻结资源不一致", nil, nil
		}
		if requirement.Audit {
			audit[requirement.Field] = printable
		}
	}
	if missing {
		return approvalv2.DecisionReviewRequired, "approval.review_required", "当前策略要求补充审批证据", nil, nil
	}
	return approvalv2.DecisionAllowed, "", "", audit, nil
}

func validateEvidence(requirement approvalv2.EvidenceRequirement, raw json.RawMessage, input approvalv2.EvaluationInput) (bool, string, error) {
	switch requirement.Kind {
	case approvalv2.EvidenceExactFactMatch:
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return false, "", err
		}
		fact, err := resolveFact(requirement.Fact, input)
		if err != nil || !fact.exists || len(fact.list) != 0 {
			return false, "", errors.New("证据匹配事实无效")
		}
		return strings.TrimSpace(value) == fact.scalar, strings.TrimSpace(value), nil
	case approvalv2.EvidenceBooleanTrue:
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil {
			return false, "", err
		}
		return value, fmt.Sprint(value), nil
	case approvalv2.EvidenceTextLength:
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return false, "", err
		}
		value = strings.TrimSpace(value)
		length := utf8.RuneCountInString(value)
		return length >= requirement.MinLength && length <= requirement.MaxLength, value, nil
	default:
		return false, "", errors.New("未知证据要求")
	}
}
