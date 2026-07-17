package nodeagent

import (
	"fmt"

	deploymentv1 "cdsoft.com.cn/VastPlan/schemas/deployment/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/shared/go/servicemodel"
)

func unitPolicy(unit deploymentv1.Unit) (servicemodel.Policy, error) {
	policy := servicemodel.Normalize(servicemodel.Policy{
		InstancePolicy: unit.InstancePolicy,
		StateModel:     unit.StateModel,
		Visibility:     unit.Visibility,
		Routing:        unit.Routing,
	})
	if err := servicemodel.Validate(policy); err != nil {
		return servicemodel.Policy{}, fmt.Errorf("unit %s 运行策略无效: %w", unit.ID, err)
	}
	if policy.InstancePolicy == servicemodel.PolicyLeader || policy.InstancePolicy == servicemodel.PolicyPartitioned {
		return servicemodel.Policy{}, fmt.Errorf("unit %s 使用 %s 策略，但当前 Node Agent 尚未实现该集群协议", unit.ID, policy.InstancePolicy)
	}
	return policy, nil
}

func contributionPolicy(contribution pluginv1.RuntimeContribution) (servicemodel.Policy, error) {
	policy := servicemodel.Policy{
		InstancePolicy: contribution.InstancePolicy,
		StateModel:     contribution.StateModel,
		Visibility:     contribution.Visibility,
		Routing:        contribution.Routing,
	}
	policy = servicemodel.Normalize(policy)
	if err := servicemodel.Validate(policy); err != nil {
		return servicemodel.Policy{}, fmt.Errorf("贡献 %s/%s 运行策略无效: %w", contribution.ExtensionPoint, contribution.ID, err)
	}
	if policy.InstancePolicy == servicemodel.PolicyLeader || policy.InstancePolicy == servicemodel.PolicyPartitioned {
		return servicemodel.Policy{}, fmt.Errorf("贡献 %s/%s 使用 %s 策略，但当前 Node Agent 尚未实现该集群协议", contribution.ExtensionPoint, contribution.ID, policy.InstancePolicy)
	}
	return policy, nil
}

func validateInstalledPolicies(unit servicemodel.Policy, plugins []InstalledPlugin) error {
	for _, plugin := range plugins {
		for _, contribution := range plugin.Contract.Contributions {
			policy, err := contributionPolicy(contribution)
			if err != nil {
				return fmt.Errorf("插件 %s: %w", plugin.ID, err)
			}
			if unit.InstancePolicy == servicemodel.PolicyPerKernel && !servicemodel.Equal(unit, policy) {
				return fmt.Errorf("插件 %s 的贡献 %s/%s 不是 per-kernel 本地策略", plugin.ID, contribution.ExtensionPoint, contribution.ID)
			}
		}
	}
	return nil
}
