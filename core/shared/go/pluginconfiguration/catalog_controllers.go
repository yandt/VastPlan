package pluginconfiguration

import (
	"errors"
	"strings"

	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func configurationControllerFor(manifest pluginv1.Manifest, unit deploymentv2.ServiceUnit, applyPath ApplyPath) (*ControllerTarget, error) {
	if manifest.Configuration == nil || manifest.Configuration.Controller == nil {
		return nil, nil
	}
	if applyPath != ApplyHotService || manifest.Configuration.Controller.Protocol != pluginv1.ConfigurationControllerProtocol {
		return nil, errors.New("controller 声明与 hot-service 路径不一致")
	}
	capability, err := pluginv1.ConfigurationControllerCapability(manifest.ID)
	if err != nil {
		return nil, err
	}
	matched, err := findControllerContribution(manifest, pluginv1.ConfigurationControllerExtensionPoint, capability)
	if err != nil {
		return nil, err
	}
	if err := validateControllerRuntimePolicy(matched, "configuration.v1"); err != nil {
		return nil, err
	}
	return controllerTarget(unit, pluginv1.ConfigurationControllerProtocol, pluginv1.ConfigurationControllerExtensionPoint, capability, matched), nil
}

func configurationResourceControllerFor(manifest pluginv1.Manifest, unit deploymentv2.ServiceUnit) (*ControllerTarget, error) {
	if manifest.Configuration == nil || manifest.Configuration.ResourceController == nil {
		return nil, nil
	}
	if manifest.Configuration.ResourceController.Protocol != pluginv1.ConfigurationResourceControllerProtocol || len(manifest.Configuration.ResourceCollections) == 0 {
		return nil, errors.New("resource controller 声明与集合契约不一致")
	}
	capability, err := pluginv1.ConfigurationResourceControllerCapability(manifest.ID)
	if err != nil {
		return nil, err
	}
	matched, err := findControllerContribution(manifest, pluginv1.ConfigurationResourceControllerExtensionPoint, capability)
	if err != nil {
		return nil, err
	}
	if err := validateControllerRuntimePolicy(matched, "configuration.resource.v1"); err != nil {
		return nil, err
	}
	return controllerTarget(unit, pluginv1.ConfigurationResourceControllerProtocol, pluginv1.ConfigurationResourceControllerExtensionPoint, capability, matched), nil
}

func findControllerContribution(manifest pluginv1.Manifest, extensionPoint, capability string) (pluginv1.RuntimeContribution, error) {
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil {
		return pluginv1.RuntimeContribution{}, err
	}
	var matched *pluginv1.RuntimeContribution
	for index := range contributions {
		contribution := &contributions[index]
		if contribution.ExtensionPoint != extensionPoint || contribution.ID != capability {
			continue
		}
		if matched != nil {
			return pluginv1.RuntimeContribution{}, errors.New("配置控制器 runtime contribution 重复")
		}
		matched = contribution
	}
	if matched == nil {
		return pluginv1.RuntimeContribution{}, errors.New("配置控制器 runtime contribution 缺失")
	}
	return *matched, nil
}

func validateControllerRuntimePolicy(contribution pluginv1.RuntimeContribution, protocol string) error {
	validLeader := contribution.InstancePolicy == "leader" && (contribution.StateModel == "leader-owned" || contribution.StateModel == "external-shared") && contribution.Routing == "leader"
	validShared := contribution.InstancePolicy == "active-active" && contribution.StateModel == "external-shared" && contribution.Routing == "queue"
	if contribution.Visibility == "local" || (!validLeader && !validShared) {
		return errors.New(protocol + " 只接受 leader 路由或 active-active queue 的外部共享状态控制器")
	}
	return nil
}

func controllerTarget(unit deploymentv2.ServiceUnit, protocol, extensionPoint, capability string, contribution pluginv1.RuntimeContribution) *ControllerTarget {
	logicalService := unit.LogicalService
	if logicalService == "" {
		logicalService = unit.ID
	}
	return &ControllerTarget{Protocol: protocol, ExtensionPoint: extensionPoint, Capability: capability, LogicalService: logicalService, RoutingDomain: contribution.RoutingDomain}
}

func validateControllerTarget(item Definition) error {
	if item.Controller == nil {
		return nil
	}
	if item.ApplyPath != ApplyHotService || item.Controller.Protocol != pluginv1.ConfigurationControllerProtocol ||
		item.Controller.ExtensionPoint != pluginv1.ConfigurationControllerExtensionPoint || strings.TrimSpace(item.Controller.LogicalService) == "" {
		return errors.New("controller 身份与生效路径不一致")
	}
	expected, err := pluginv1.ConfigurationControllerCapability(item.PluginID)
	if err != nil || item.Controller.Capability != expected {
		return errors.New("controller capability 与签名插件身份不一致")
	}
	return nil
}

func validateResourceControllerTarget(item Definition) error {
	if item.ResourceController == nil {
		return nil
	}
	target := item.ResourceController
	if target.Protocol != pluginv1.ConfigurationResourceControllerProtocol ||
		target.ExtensionPoint != pluginv1.ConfigurationResourceControllerExtensionPoint || strings.TrimSpace(target.LogicalService) == "" {
		return errors.New("resource controller 身份无效")
	}
	expected, err := pluginv1.ConfigurationResourceControllerCapability(item.PluginID)
	if err != nil || target.Capability != expected {
		return errors.New("resource controller capability 与签名插件身份不一致")
	}
	return nil
}
