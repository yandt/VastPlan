package backendcompositionv1

import "fmt"

func validateConfigurationPlan(plan ConfigurationPlan) error {
	seen := map[string]struct{}{}
	for _, item := range plan.Items {
		key := item.UnitID + "\x00" + item.PluginID
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("Configuration Plan item 重复: %s/%s", item.UnitID, item.PluginID)
		}
		seen[key] = struct{}{}
		if len(item.DependencyPath) == 0 || item.DependencyPath[len(item.DependencyPath)-1] != item.PluginID {
			return fmt.Errorf("Configuration Plan %s/%s dependencyPath 必须以目标插件结束", item.UnitID, item.PluginID)
		}
		switch item.Source {
		case "root":
			if len(item.DependencyPath) != 1 {
				return fmt.Errorf("Configuration Plan root %s/%s dependencyPath 只能包含自身", item.UnitID, item.PluginID)
			}
		case "package-dependency":
			if len(item.DependencyPath) < 2 {
				return fmt.Errorf("Configuration Plan package dependency %s/%s 必须包含根到依赖的完整路径", item.UnitID, item.PluginID)
			}
		case "platform-provider", "foundation":
			if item.Editable {
				return fmt.Errorf("Configuration Plan %s/%s 的平台配置不能由应用编辑", item.UnitID, item.PluginID)
			}
		}
		missing := map[string]struct{}{}
		for _, requirement := range item.Missing {
			key := requirement.Kind + "\x00" + requirement.Field
			if _, duplicate := missing[key]; duplicate {
				return fmt.Errorf("Configuration Plan %s/%s 缺失项重复: %s/%s", item.UnitID, item.PluginID, requirement.Kind, requirement.Field)
			}
			missing[key] = struct{}{}
		}
	}
	return nil
}

func validateReportReferences(report ResolutionReport) error {
	nodes := make(map[string]ServiceDependencyNode, len(report.ServiceGraph.Nodes))
	for _, node := range report.ServiceGraph.Nodes {
		nodes[node.UnitID] = node
	}
	unitPlugins := map[string]map[string]struct{}{}
	if report.ApplicationComposition != nil {
		for _, unit := range report.ApplicationComposition.Units {
			node, exists := nodes[unit.Spec.ID]
			if !exists || node.ServiceClass != unit.ServiceClass {
				return fmt.Errorf("Resolution Report service graph 缺少 Application Composition unit %q", unit.Spec.ID)
			}
			plugins := make(map[string]struct{}, len(unit.Spec.Plugins))
			for _, plugin := range unit.Spec.Plugins {
				plugins[plugin.ID] = struct{}{}
			}
			unitPlugins[unit.Spec.ID] = plugins
		}
	}
	for _, feature := range report.Features {
		if _, exists := nodes[feature.UnitID]; !exists {
			return fmt.Errorf("Resolution Report Feature 引用未知 unit %q", feature.UnitID)
		}
		if plugins := unitPlugins[feature.UnitID]; plugins != nil {
			if _, exists := plugins[feature.PluginID]; !exists {
				return fmt.Errorf("Resolution Report Feature 引用 unit %q 中不存在的插件 %q", feature.UnitID, feature.PluginID)
			}
		}
	}
	for _, binding := range report.ProviderBindings {
		if _, exists := nodes[binding.ConsumerUnitID]; !exists {
			return fmt.Errorf("Resolution Report Provider Binding 引用未知 consumer unit %q", binding.ConsumerUnitID)
		}
		if _, exists := nodes[binding.ProviderUnitID]; !exists {
			return fmt.Errorf("Resolution Report Provider Binding 引用未知 provider unit %q", binding.ProviderUnitID)
		}
		if plugins := unitPlugins[binding.ProviderUnitID]; plugins != nil {
			if _, exists := plugins[binding.ProviderPluginID]; !exists {
				return fmt.Errorf("Resolution Report Provider Binding 引用 provider unit %q 中不存在的插件 %q", binding.ProviderUnitID, binding.ProviderPluginID)
			}
		}
	}
	for _, item := range report.ConfigurationPlan.Items {
		if _, exists := nodes[item.UnitID]; !exists {
			return fmt.Errorf("Configuration Plan 引用未知 unit %q", item.UnitID)
		}
		if plugins := unitPlugins[item.UnitID]; plugins != nil {
			if _, exists := plugins[item.PluginID]; !exists {
				return fmt.Errorf("Configuration Plan 引用 unit %q 中不存在的插件 %q", item.UnitID, item.PluginID)
			}
		}
	}
	return nil
}

func validateResolutionStatus(report ResolutionReport) error {
	hasMissing := false
	for _, item := range report.ConfigurationPlan.Items {
		hasMissing = hasMissing || len(item.Missing) > 0
	}
	hasError := false
	for _, diagnostic := range report.Diagnostics {
		hasError = hasError || diagnostic.Severity == "error"
	}
	switch report.Status {
	case ResolutionResolved:
		if hasMissing || hasError {
			return fmt.Errorf("Resolved 报告不能包含配置缺失或 error diagnostic")
		}
	case ResolutionNeedsConfiguration:
		if !hasMissing || hasError {
			return fmt.Errorf("NeedsConfiguration 报告必须仅包含配置缺失，不能包含 error diagnostic")
		}
	case ResolutionInvalid:
		if !hasError {
			return fmt.Errorf("Invalid 报告必须包含 error diagnostic")
		}
	default:
		return fmt.Errorf("未知 Resolution Report status %q", report.Status)
	}
	return nil
}
