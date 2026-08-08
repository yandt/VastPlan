package runtimehost

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var kernelServiceNamePattern = regexp.MustCompile(`^kernel\.[a-z0-9][a-z0-9._-]{0,159}$`)

type kernelServiceCeiling struct {
	all    bool
	values map[string]struct{}
}

// KernelServiceGrantPolicy is controlled by the kernel operator. A signed
// manifest can request services, but only this publisher ceiling and the exact
// service-unit grant can authorize them for one launch.
type KernelServiceGrantPolicy struct {
	defaultCeiling    kernelServiceCeiling
	publisherCeilings map[string]kernelServiceCeiling
}

func DefaultKernelServiceGrantPolicy() KernelServiceGrantPolicy {
	return KernelServiceGrantPolicy{
		defaultCeiling:    kernelServiceCeiling{values: map[string]struct{}{}},
		publisherCeilings: map[string]kernelServiceCeiling{"vastplan": {all: true}},
	}
}

// ParseKernelServiceGrantPolicy parses comma-separated services and
// semicolon-separated publisher=service,service rules. "*" is valid only in
// an operator ceiling; a Platform Profile must always enumerate exact grants.
func ParseKernelServiceGrantPolicy(defaultServices, publisherRules string) (KernelServiceGrantPolicy, error) {
	policy := DefaultKernelServiceGrantPolicy()
	if strings.TrimSpace(defaultServices) != "" {
		ceiling, err := parseKernelServiceCeiling(defaultServices)
		if err != nil {
			return KernelServiceGrantPolicy{}, fmt.Errorf("默认 Kernel Service 上限: %w", err)
		}
		policy.defaultCeiling = ceiling
	}
	if strings.TrimSpace(publisherRules) == "" {
		return policy, nil
	}
	seen := map[string]struct{}{}
	for _, rawRule := range strings.Split(publisherRules, ";") {
		publisher, services, ok := strings.Cut(rawRule, "=")
		publisher = strings.TrimSpace(publisher)
		if !ok || publisher == "" || strings.TrimSpace(services) == "" {
			return KernelServiceGrantPolicy{}, fmt.Errorf("发布者 Kernel Service 策略格式无效: %q", rawRule)
		}
		if _, duplicate := seen[publisher]; duplicate {
			return KernelServiceGrantPolicy{}, fmt.Errorf("发布者 Kernel Service 策略重复: %s", publisher)
		}
		seen[publisher] = struct{}{}
		ceiling, err := parseKernelServiceCeiling(services)
		if err != nil {
			return KernelServiceGrantPolicy{}, fmt.Errorf("发布者 %s Kernel Service 上限: %w", publisher, err)
		}
		policy.publisherCeilings[publisher] = ceiling
	}
	return policy, nil
}

func parseKernelServiceCeiling(raw string) (kernelServiceCeiling, error) {
	if strings.TrimSpace(raw) == "*" {
		return kernelServiceCeiling{all: true}, nil
	}
	values, err := normalizeKernelServices(strings.Split(raw, ","))
	if err != nil {
		return kernelServiceCeiling{}, err
	}
	return kernelServiceCeiling{values: serviceSet(values)}, nil
}

// Compile creates the exact grant list for a plugin. platformConfigured
// distinguishes an explicit empty grant from an omitted Platform Profile
// decision. Kernel services declared by a manifest are required capabilities;
// a partial grant therefore blocks activation instead of producing a plugin
// that fails later at runtime.
func (p KernelServiceGrantPolicy) Compile(pluginID, publisher string, requested, platformGrants []string, platformConfigured bool) ([]string, error) {
	requests, err := normalizeKernelServices(requested)
	if err != nil {
		return nil, fmt.Errorf("插件 %s Kernel Service 申请: %w", pluginID, err)
	}
	grants, err := normalizeKernelServices(platformGrants)
	if err != nil {
		return nil, fmt.Errorf("插件 %s Platform Grant: %w", pluginID, err)
	}
	if len(requests) == 0 {
		if len(grants) != 0 {
			return nil, fmt.Errorf("插件 %s 未申请 Kernel Service，但 Platform Profile 授予了 %v", pluginID, grants)
		}
		return nil, nil
	}
	if !platformConfigured {
		return nil, fmt.Errorf("插件 %s 申请了 %v，但 Platform Profile 未配置 config.kernel_service_grants", pluginID, requests)
	}
	requestedSet, grantedSet := serviceSet(requests), serviceSet(grants)
	for _, service := range grants {
		if _, requested := requestedSet[service]; !requested {
			return nil, fmt.Errorf("插件 %s 的 Platform Grant %q 未在签名 Manifest 中申请", pluginID, service)
		}
	}
	ceiling := p.defaultCeiling
	if configured, ok := p.publisherCeilings[publisher]; ok {
		ceiling = configured
	}
	for _, service := range requests {
		if _, granted := grantedSet[service]; !granted {
			return nil, fmt.Errorf("插件 %s 申请的 Kernel Service %q 未获 Platform Profile 授权", pluginID, service)
		}
		if !ceiling.all {
			if _, allowed := ceiling.values[service]; !allowed {
				return nil, fmt.Errorf("发布者 %s 不允许插件 %s 使用 Kernel Service %q", publisher, pluginID, service)
			}
		}
	}
	return requests, nil
}

func normalizeKernelServices(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		if !kernelServiceNamePattern.MatchString(value) {
			return nil, fmt.Errorf("Kernel Service 名称 %q 无效", value)
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, fmt.Errorf("Kernel Service %q 重复", value)
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func serviceSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
