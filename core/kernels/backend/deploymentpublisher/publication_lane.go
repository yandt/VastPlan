package deploymentpublisher

import (
	"errors"
	"fmt"
	"reflect"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
)

type PublicationLane string

const (
	PublicationLaneApplication PublicationLane = "application"
	PublicationLaneBootstrap   PublicationLane = "bootstrap"
	PublicationLaneSeed        PublicationLane = "seed"
)

func ValidatePublicationLane(lane PublicationLane, current *deploymentv2.Deployment, next deploymentv2.Deployment) error {
	switch lane {
	case PublicationLaneSeed:
		return nil
	case PublicationLaneApplication:
		return validateApplicationPublication(current, next)
	case PublicationLaneBootstrap:
		return validateBootstrapPublication(current, next)
	default:
		return errors.New("Deployment publication lane 未配置")
	}
}

func validateApplicationPublication(current *deploymentv2.Deployment, next deploymentv2.Deployment) error {
	if current == nil {
		for _, unit := range next.Units {
			if unit.StartupTier == "bootstrap" {
				return errors.New("普通发布不能创建 Bootstrap 单元；必须使用可信 Seed 或 Bootstrap 换版通道")
			}
		}
		return nil
	}
	before, after := serviceUnitsByID(current.Units), serviceUnitsByID(next.Units)
	for id := range unionUnitIDs(before, after) {
		old, oldOK := before[id]
		candidate, candidateOK := after[id]
		protected := oldOK && old.StartupTier == "bootstrap" || candidateOK && candidate.StartupTier == "bootstrap"
		if protected && (!oldOK || !candidateOK || !reflect.DeepEqual(old, candidate)) {
			return fmt.Errorf("普通发布不能修改 Bootstrap 单元 %q；必须使用可信 Bootstrap 换版通道", id)
		}
	}
	return nil
}

func validateBootstrapPublication(current *deploymentv2.Deployment, next deploymentv2.Deployment) error {
	if current == nil {
		return errors.New("Bootstrap 换版通道不能创建初始 Deployment；首次发布必须使用可信 Seed")
	}
	if current.Metadata != next.Metadata || current.Version != next.Version {
		return errors.New("Bootstrap 换版不能改变 Deployment identity 或 schema version")
	}
	if !reflect.DeepEqual(current.AppProfiles, next.AppProfiles) ||
		current.Resolution.ApplicationComposition != next.Resolution.ApplicationComposition ||
		current.Resolution.DevelopmentMode != next.Resolution.DevelopmentMode ||
		!reflect.DeepEqual(current.Resolution.PluginOrigins, next.Resolution.PluginOrigins) ||
		!reflect.DeepEqual(current.Resolution.SchemaActivation, next.Resolution.SchemaActivation) {
		return errors.New("Bootstrap 换版只能改变 Bootstrap 单元插件版本，不能改变应用组合、App Profile 或发布策略")
	}

	before, after := serviceUnitsByID(current.Units), serviceUnitsByID(next.Units)
	if len(before) != len(after) {
		return errors.New("Bootstrap 换版不能增加或删除 ServiceUnit")
	}
	changedPlugins := map[string]struct{}{}
	for id, old := range before {
		candidate, ok := after[id]
		if !ok {
			return fmt.Errorf("Bootstrap 换版不能删除 ServiceUnit %q", id)
		}
		if old.StartupTier != "bootstrap" {
			if !reflect.DeepEqual(old, candidate) {
				return fmt.Errorf("Bootstrap 换版不能修改 Full 单元 %q", id)
			}
			continue
		}
		if candidate.StartupTier != "bootstrap" {
			return fmt.Errorf("Bootstrap 换版不能改变单元 %q 的 startup_tier", id)
		}
		oldShape, candidateShape := old, candidate
		oldShape.Plugins, candidateShape.Plugins = nil, nil
		if !reflect.DeepEqual(oldShape, candidateShape) {
			return fmt.Errorf("Bootstrap 换版不能改变单元 %q 的配置、拓扑、资源或副本策略", id)
		}
		oldPlugins, candidatePlugins := pluginRefsByID(old.Plugins), pluginRefsByID(candidate.Plugins)
		if len(oldPlugins) != len(candidatePlugins) {
			return fmt.Errorf("Bootstrap 换版不能增加或删除单元 %q 的插件", id)
		}
		for pluginID, oldRef := range oldPlugins {
			candidateRef, exists := candidatePlugins[pluginID]
			if !exists {
				return fmt.Errorf("Bootstrap 换版不能替换单元 %q 的插件身份 %q", id, pluginID)
			}
			if oldRef != candidateRef {
				changedPlugins[pluginID] = struct{}{}
			}
		}
	}
	if len(changedPlugins) == 0 {
		return errors.New("Bootstrap 换版请求没有改变任何 Bootstrap 插件版本")
	}
	if err := validateBootstrapBaselineChanges(current.Resolution.PluginBaselines, next.Resolution.PluginBaselines, changedPlugins); err != nil {
		return err
	}
	return nil
}

func validateBootstrapBaselineChanges(before, after map[string]string, changed map[string]struct{}) error {
	keys := map[string]struct{}{}
	for key := range before {
		keys[key] = struct{}{}
	}
	for key := range after {
		keys[key] = struct{}{}
	}
	for key := range keys {
		if before[key] == after[key] {
			continue
		}
		if _, allowed := changed[key]; !allowed {
			return fmt.Errorf("Bootstrap 换版不能改变非目标插件 %q 的 baseline", key)
		}
	}
	return nil
}

func serviceUnitsByID(units []deploymentv2.ServiceUnit) map[string]deploymentv2.ServiceUnit {
	result := make(map[string]deploymentv2.ServiceUnit, len(units))
	for _, unit := range units {
		result[unit.ID] = unit
	}
	return result
}

func unionUnitIDs(before, after map[string]deploymentv2.ServiceUnit) map[string]struct{} {
	result := make(map[string]struct{}, len(before)+len(after))
	for id := range before {
		result[id] = struct{}{}
	}
	for id := range after {
		result[id] = struct{}{}
	}
	return result
}

func pluginRefsByID(refs []deploymentv1.PluginRef) map[string]deploymentv1.PluginRef {
	result := make(map[string]deploymentv1.PluginRef, len(refs))
	for _, ref := range refs {
		result[ref.ID] = ref
	}
	return result
}
