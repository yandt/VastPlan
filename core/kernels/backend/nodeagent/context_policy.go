package nodeagent

import (
	"fmt"
	"strings"

	"cdsoft.com.cn/VastPlan/core/shared/go/callcontext"
)

// ContextPolicy is configured by the kernel user. Exact publisher overrides
// take precedence over the global ceiling; signed plugin manifests can only
// request a subset and can never widen these sets.
type ContextPolicy struct {
	DefaultAccess     callcontext.AccessSet
	PublisherAccesses map[string]callcontext.AccessSet
}

func DefaultContextPolicy() ContextPolicy {
	isolated := callcontext.MustAccess(
		callcontext.FieldScopeTenant, callcontext.FieldScopeProject, callcontext.FieldCaller,
		callcontext.FieldScene, callcontext.FieldSubjectID, callcontext.FieldTrace,
		callcontext.FieldRequestDeadline, callcontext.FieldRequestIdempotency,
		callcontext.FieldPropagationPath,
	)
	firstParty := callcontext.MustAccess(
		callcontext.FieldScopeTenant, callcontext.FieldScopeProject, callcontext.FieldCaller,
		callcontext.FieldScene, callcontext.FieldSubjectID, callcontext.FieldSubjectProfile,
		callcontext.FieldAuthorizationRole, callcontext.FieldAuthorizationAdmin,
		callcontext.FieldTrace, callcontext.FieldRequestDeadline, callcontext.FieldRequestIdempotency,
		callcontext.FieldGrantCredentials, callcontext.FieldBaggage, callcontext.FieldPropagationPath,
	)
	return ContextPolicy{DefaultAccess: isolated, PublisherAccesses: map[string]callcontext.AccessSet{"vastplan": firstParty}}
}

func NewContextPolicy(defaultFields []string, publishers map[string][]string) (ContextPolicy, error) {
	defaultAccess, err := callcontext.ParseAccess(defaultFields)
	if err != nil {
		return ContextPolicy{}, fmt.Errorf("默认发布者上下文策略: %w", err)
	}
	policy := ContextPolicy{DefaultAccess: defaultAccess, PublisherAccesses: map[string]callcontext.AccessSet{}}
	for publisher, fields := range publishers {
		if publisher == "" {
			return ContextPolicy{}, fmt.Errorf("发布者上下文策略名称不能为空")
		}
		access, err := callcontext.ParseAccess(fields)
		if err != nil {
			return ContextPolicy{}, fmt.Errorf("发布者 %s 上下文策略: %w", publisher, err)
		}
		policy.PublisherAccesses[publisher] = access
	}
	return policy, nil
}

// ParseContextPolicy parses production flags. Fields are comma-separated;
// publisher rules are semicolon-separated publisher=field,field pairs. "*"
// grants the complete known field set but still cannot bypass manifest or
// extension-point ceilings.
func ParseContextPolicy(defaultFields, publisherRules string) (ContextPolicy, error) {
	policy := DefaultContextPolicy()
	if strings.TrimSpace(defaultFields) != "" {
		access, err := parseContextFields(defaultFields)
		if err != nil {
			return ContextPolicy{}, fmt.Errorf("默认上下文上限: %w", err)
		}
		policy.DefaultAccess = access
	}
	if strings.TrimSpace(publisherRules) == "" {
		return policy, nil
	}
	seenRules := map[string]struct{}{}
	for _, rawRule := range strings.Split(publisherRules, ";") {
		publisher, fields, ok := strings.Cut(rawRule, "=")
		publisher = strings.TrimSpace(publisher)
		if !ok || publisher == "" || strings.TrimSpace(fields) == "" {
			return ContextPolicy{}, fmt.Errorf("发布者上下文策略格式无效: %q", rawRule)
		}
		if _, duplicate := seenRules[publisher]; duplicate {
			return ContextPolicy{}, fmt.Errorf("发布者上下文策略重复: %s", publisher)
		}
		seenRules[publisher] = struct{}{}
		access, err := parseContextFields(fields)
		if err != nil {
			return ContextPolicy{}, fmt.Errorf("发布者 %s 上下文上限: %w", publisher, err)
		}
		policy.PublisherAccesses[publisher] = access
	}
	return policy, nil
}

func parseContextFields(raw string) (callcontext.AccessSet, error) {
	if strings.TrimSpace(raw) == "*" {
		return callcontext.AllFields(), nil
	}
	parts := strings.Split(raw, ",")
	fields := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("上下文字段不能为空")
		}
		fields = append(fields, part)
	}
	return callcontext.ParseAccess(fields)
}

func (p ContextPolicy) Ceiling(publisher string) callcontext.AccessSet {
	if access, ok := p.PublisherAccesses[publisher]; ok {
		return access.Clone()
	}
	if p.DefaultAccess != nil {
		return p.DefaultAccess.Clone()
	}
	return DefaultContextPolicy().DefaultAccess.Clone()
}
