package pluginv1

import (
	"fmt"

	"cdsoft.com.cn/VastPlan/core/shared/go/servicemodel"
)

func runtimePolicies(manifest Manifest) (servicemodel.Policy, map[string]RuntimeCapabilityPolicy, error) {
	if manifest.Runtime == nil {
		return servicemodel.Normalize(servicemodel.Policy{}), nil, nil
	}
	policy := servicemodel.Policy{
		InstancePolicy: manifest.Runtime.InstancePolicy,
		StateModel:     manifest.Runtime.StateModel,
		Visibility:     manifest.Runtime.Visibility,
		Routing:        manifest.Runtime.Routing,
		RoutingDomain:  manifest.Runtime.RoutingDomain,
	}
	policy = servicemodel.Normalize(policy)
	if err := servicemodel.Validate(policy); err != nil {
		return servicemodel.Policy{}, nil, fmt.Errorf("runtime 策略无效: %w", err)
	}
	overrides := make(map[string]RuntimeCapabilityPolicy, len(manifest.Runtime.Provides))
	for _, provide := range manifest.Runtime.Provides {
		if provide.ExtensionPoint == "" || provide.Capability == "" {
			return servicemodel.Policy{}, nil, fmt.Errorf("runtime.provides 必须填写 extensionPoint 和 capability")
		}
		key := provide.ExtensionPoint + "\x00" + provide.Capability
		if _, exists := overrides[key]; exists {
			return servicemodel.Policy{}, nil, fmt.Errorf("runtime.provides 重复: %s/%s", provide.ExtensionPoint, provide.Capability)
		}
		override := provide
		overridePolicy := policy
		overridePolicy.Visibility = provide.Visibility
		overridePolicy.Routing = provide.Routing
		overridePolicy.RoutingDomain = provide.RoutingDomain
		overridePolicy = servicemodel.Normalize(overridePolicy)
		if err := servicemodel.Validate(overridePolicy); err != nil {
			return servicemodel.Policy{}, nil, fmt.Errorf("runtime.provides %s/%s 策略无效: %w", provide.ExtensionPoint, provide.Capability, err)
		}
		override.Visibility = overridePolicy.Visibility
		override.Routing = overridePolicy.Routing
		override.RoutingDomain = overridePolicy.RoutingDomain
		overrides[key] = override
	}
	if err := validateRuntimeRequirements(manifest.Runtime.Requires); err != nil {
		return servicemodel.Policy{}, nil, err
	}
	return policy, overrides, nil
}

func validateRuntimeRequirements(requirements []RuntimeRequirement) error {
	seen := make(map[string]struct{}, len(requirements))
	for _, requirement := range requirements {
		if requirement.Capability == "" {
			return fmt.Errorf("runtime.requires capability 不能为空")
		}
		if requirement.Scope != "same-node" && requirement.Scope != "same-kernel" && requirement.Scope != "remote" {
			return fmt.Errorf("runtime.requires %s scope 无效: %q", requirement.Capability, requirement.Scope)
		}
		if requirement.Kind != "strong" && requirement.Kind != "soft" && requirement.Kind != "lazy" && requirement.Kind != "data" {
			return fmt.Errorf("runtime.requires %s kind 无效: %q", requirement.Capability, requirement.Kind)
		}
		if requirement.Ready != "readiness" && requirement.Ready != "health" {
			return fmt.Errorf("runtime.requires %s ready 无效: %q", requirement.Capability, requirement.Ready)
		}
		if requirement.FailurePolicy != "fail" && requirement.FailurePolicy != "degrade" && requirement.FailurePolicy != "retry" {
			return fmt.Errorf("runtime.requires %s failurePolicy 无效: %q", requirement.Capability, requirement.FailurePolicy)
		}
		key := requirement.Capability + "\x00" + requirement.LogicalService + "\x00" + requirement.RoutingDomain
		if _, exists := seen[key]; exists {
			return fmt.Errorf("runtime.requires 重复: %s", requirement.Capability)
		}
		seen[key] = struct{}{}
	}
	return nil
}
