package approvalv2

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

var (
	governedID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$`)
	routingID  = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	digest     = regexp.MustCompile(`^[a-f0-9]{64}$`)
	factRef    = regexp.MustCompile(`^(operation|actor\.(id|roles)|resource\.(id|digest|submittedBy)|resource\.attributes\.[a-z][a-z0-9._-]*|context\.[a-z][a-z0-9._-]*)$`)
)

func ValidateBinding(value ProviderBinding) error {
	if value.Protocol != Protocol || !governedID.MatchString(value.Capability) || !governedID.MatchString(value.LogicalService) || !routingID.MatchString(value.RoutingDomain) {
		return errors.New("Approval Provider Binding 协议或寻址无效")
	}
	return ValidateProfileRef(value.Profile)
}

func ValidateProfileRef(value ProfileRef) error {
	if !governedID.MatchString(value.ID) || value.Revision == 0 || !digest.MatchString(value.Digest) {
		return errors.New("Approval Policy ProfileRef 无效")
	}
	return nil
}

func ValidateProfile(profile PolicyProfile) error {
	if !governedID.MatchString(profile.ID) || profile.Revision == 0 || profile.DefaultEffect != EffectDeny {
		return errors.New("Native Approval Profile 必须使用有效 ID/revision 和 deny 默认效果")
	}
	if len(profile.Rules) == 0 || len(profile.Rules) > 256 {
		return errors.New("Native Approval Profile rules 数量必须为 1 至 256")
	}
	seen := map[string]struct{}{}
	for _, rule := range profile.Rules {
		if !governedID.MatchString(rule.ID) {
			return fmt.Errorf("Approval Rule ID 无效: %s", rule.ID)
		}
		if _, ok := seen[rule.ID]; ok {
			return fmt.Errorf("Approval Rule 重复: %s", rule.ID)
		}
		seen[rule.ID] = struct{}{}
		if len(rule.Conditions) == 0 || len(rule.Conditions) > 32 {
			return fmt.Errorf("Approval Rule %s conditions 数量无效", rule.ID)
		}
		if rule.Effect != EffectAllow && rule.Effect != EffectDeny && rule.Effect != EffectRequireEvidence {
			return fmt.Errorf("Approval Rule %s effect 无效", rule.ID)
		}
		if rule.Effect == EffectRequireEvidence && len(rule.Requirements) == 0 {
			return fmt.Errorf("Approval Rule %s 缺少证据要求", rule.ID)
		}
		if rule.Effect != EffectRequireEvidence && len(rule.Requirements) != 0 {
			return fmt.Errorf("Approval Rule %s 只有 require-evidence 可声明 requirements", rule.ID)
		}
		for _, condition := range rule.Conditions {
			if err := validateCondition(condition); err != nil {
				return fmt.Errorf("Approval Rule %s: %w", rule.ID, err)
			}
		}
		for _, requirement := range rule.Requirements {
			if err := validateRequirement(requirement); err != nil {
				return fmt.Errorf("Approval Rule %s: %w", rule.ID, err)
			}
		}
	}
	return nil
}

func validateCondition(value Condition) error {
	if !factRef.MatchString(value.Left) {
		return fmt.Errorf("事实引用无效: %s", value.Left)
	}
	switch value.Operator {
	case OperatorExists, OperatorNotExists:
		if value.RightFact != "" || value.Value != "" {
			return errors.New("exists 条件不得声明右值")
		}
	case OperatorEquals, OperatorNotEquals, OperatorContains, OperatorNotContains:
		if (value.RightFact == "") == (value.Value == "") {
			return errors.New("比较条件必须且只能声明 rightFact 或 value")
		}
		if value.RightFact != "" && !factRef.MatchString(value.RightFact) {
			return fmt.Errorf("右侧事实引用无效: %s", value.RightFact)
		}
	default:
		return fmt.Errorf("操作符无效: %s", value.Operator)
	}
	return nil
}

func validateRequirement(value EvidenceRequirement) error {
	if !governedID.MatchString(value.ID) || !governedID.MatchString(value.Field) {
		return errors.New("证据要求 ID 或 field 无效")
	}
	switch value.Kind {
	case EvidenceExactFactMatch:
		if !factRef.MatchString(value.Fact) {
			return errors.New("exact-fact-match 缺少有效 fact")
		}
	case EvidenceBooleanTrue:
		if value.Fact != "" {
			return errors.New("boolean-true 不得声明 fact")
		}
	case EvidenceTextLength:
		if value.MinLength < 1 || value.MaxLength < value.MinLength || value.MaxLength > 4096 {
			return errors.New("text-length 范围无效")
		}
	default:
		return errors.New("证据要求 kind 无效")
	}
	return nil
}

func ValidateInput(value EvaluationInput) error {
	if !governedID.MatchString(value.Operation) || strings.TrimSpace(value.TenantID) == "" || strings.TrimSpace(value.Actor.ID) == "" || strings.TrimSpace(value.Resource.ID) == "" || !digest.MatchString(value.Resource.Digest) {
		return errors.New("Approval EvaluationInput 缺少可信 operation/tenant/actor/resource/digest")
	}
	if len(value.Resource.Attributes) > 32 || len(value.Context) > 32 || len(value.Evidence) > 32 {
		return errors.New("Approval EvaluationInput 扩展字段超过上限")
	}
	for key, raw := range value.Evidence {
		if !governedID.MatchString(key) || len(raw) > 8192 || !json.Valid(raw) {
			return errors.New("Approval evidence 字段无效")
		}
	}
	return nil
}

func ValidateDecision(value Decision) error {
	if err := ValidateProfileRef(value.Profile); err != nil {
		return err
	}
	if value.Status != DecisionAllowed && value.Status != DecisionReviewRequired && value.Status != DecisionDenied {
		return errors.New("Approval Decision status 无效")
	}
	if value.MatchedRuleID != "" && !governedID.MatchString(value.MatchedRuleID) {
		return errors.New("Approval Decision matchedRuleId 无效")
	}
	if value.Code != "" && !governedID.MatchString(value.Code) {
		return errors.New("Approval Decision code 无效")
	}
	if len(value.Message) > 1024 || len(value.AuditNote) > 16384 || len(value.Requirements) > 32 {
		return errors.New("Approval Decision 输出超过上限")
	}
	if value.Status == DecisionReviewRequired && len(value.Requirements) == 0 {
		return errors.New("review-required Decision 缺少证据要求")
	}
	for _, requirement := range value.Requirements {
		if err := validateRequirement(requirement); err != nil {
			return fmt.Errorf("Approval Decision evidence requirement: %w", err)
		}
	}
	return nil
}

func ProfileDigest(profile PolicyProfile) (string, error) {
	if err := ValidateProfile(profile); err != nil {
		return "", err
	}
	normalized := profile
	normalized.Rules = append([]Rule(nil), profile.Rules...)
	sort.Slice(normalized.Rules, func(i, j int) bool {
		if normalized.Rules[i].Priority != normalized.Rules[j].Priority {
			return normalized.Rules[i].Priority > normalized.Rules[j].Priority
		}
		return normalized.Rules[i].ID < normalized.Rules[j].ID
	})
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	value := sha256.Sum256(raw)
	return hex.EncodeToString(value[:]), nil
}

func RefForProfile(profile PolicyProfile) (ProfileRef, error) {
	digest, err := ProfileDigest(profile)
	if err != nil {
		return ProfileRef{}, err
	}
	return ProfileRef{ID: profile.ID, Revision: profile.Revision, Digest: digest}, nil
}

func DecodeStrict(raw []byte, target any) error {
	if len(raw) == 0 || len(raw) > 4<<20 {
		return errors.New("Approval Policy payload 大小无效")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("Approval Policy payload 只能包含一个 JSON document")
	}
	return nil
}
