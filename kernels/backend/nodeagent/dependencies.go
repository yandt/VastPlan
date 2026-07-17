package nodeagent

import (
	"context"
	"fmt"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/shared/go/addressing"
	"cdsoft.com.cn/VastPlan/shared/go/servicemodel"
	"github.com/Masterminds/semver/v3"
)

// validateRuntimeRequirements 只检查签名清单声明的运行时依赖。strong/data 在
// readiness 未满足时阻断候选；soft 或 failurePolicy=degrade 允许启动，但返回诊断。
func validateRuntimeRequirements(ctx context.Context, plugins []InstalledPlugin, router *addressing.Router, timeout time.Duration) ([]string, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	local := make(map[string][]string)
	for _, plugin := range plugins {
		for _, contribution := range plugin.Contract.Contributions {
			policy, err := contributionPolicy(contribution)
			if err != nil {
				return nil, err
			}
			local[contribution.ID] = append(local[contribution.ID], plugin.Version)
			if policy.Visibility != servicemodel.VisibilityLocal && contribution.InstancePolicy == servicemodel.PolicyPerKernel {
				return nil, fmt.Errorf("贡献 %s/%s 声明 per-kernel 但 visibility 不是 local", contribution.ExtensionPoint, contribution.ID)
			}
		}
	}
	var degraded []string
	for _, plugin := range plugins {
		for _, requirement := range plugin.Contract.Requires {
			if requirement.Kind == "lazy" {
				continue
			}
			ok, err := dependencyReady(ctx, requirement, local, router, timeout)
			if err != nil {
				if requirement.FailurePolicy == "degrade" || requirement.Kind == "soft" {
					degraded = append(degraded, plugin.ID+"->"+requirement.Capability+": "+err.Error())
					continue
				}
				return degraded, fmt.Errorf("插件 %s 依赖 %s 未就绪: %w", plugin.ID, requirement.Capability, err)
			}
			if !ok {
				message := "未找到可用 readiness lease"
				if requirement.FailurePolicy == "degrade" || requirement.Kind == "soft" {
					degraded = append(degraded, plugin.ID+"->"+requirement.Capability+": "+message)
					continue
				}
				return degraded, fmt.Errorf("插件 %s 依赖 %s 未就绪", plugin.ID, requirement.Capability)
			}
		}
	}
	return degraded, nil
}

func dependencyReady(ctx context.Context, requirement pluginv1.RuntimeRequirement, local map[string][]string, router *addressing.Router, timeout time.Duration) (bool, error) {
	if requirement.Scope == "same-node" || requirement.Scope == "same-kernel" {
		if router != nil && router.HasLocal(requirement.Capability) {
			return true, nil
		}
		return versionsMatch(local[requirement.Capability], requirement.Version), nil
	}
	if router == nil {
		return false, fmt.Errorf("远端 addressing router 未接入")
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		for _, instance := range router.InstancesFor(requirement.Capability, requirement.LogicalService, requirement.RoutingDomain) {
			if requirement.Ready == "health" || instance.Readiness == "" || instance.Readiness == "ready" {
				if versionsMatch([]string{instance.Version}, requirement.Version) {
					return true, nil
				}
			}
		}
		select {
		case <-waitCtx.Done():
			return false, waitCtx.Err()
		case <-ticker.C:
		}
	}
}

func versionsMatch(versions []string, constraint string) bool {
	if len(versions) == 0 {
		return false
	}
	if constraint == "" {
		return true
	}
	rangeConstraint, err := semver.NewConstraint(constraint)
	if err != nil {
		return false
	}
	for _, version := range versions {
		parsed, err := semver.NewVersion(version)
		if err == nil && rangeConstraint.Check(parsed) {
			return true
		}
	}
	return false
}
