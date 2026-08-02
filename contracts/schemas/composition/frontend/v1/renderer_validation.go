package frontendcompositionv1

import (
	"encoding/json"
	"fmt"
	"strings"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

func ParsePlatformProfile(raw []byte) (PlatformProfile, error) {
	p, _, _, err := schemas()
	if err != nil {
		return PlatformProfile{}, err
	}
	if err := validateJSON(p, raw, "Frontend Platform Profile"); err != nil {
		return PlatformProfile{}, err
	}
	var value PlatformProfile
	if err := json.Unmarshal(raw, &value); err != nil {
		return PlatformProfile{}, err
	}
	if err := compositioncommonv1.ValidateTarget(value.Target, compositioncommonv1.KernelFrontend); err != nil {
		return PlatformProfile{}, err
	}
	value.Plugins, err = normalizeRefs(value.Plugins)
	if err != nil {
		return PlatformProfile{}, err
	}
	value.RuntimeEngine.Channel = channel(value.RuntimeEngine.Channel)
	value.RenderAdapter.Channel = channel(value.RenderAdapter.Channel)
	value.Shell.Channel = channel(value.Shell.Channel)
	value.Workbench.Channel = channel(value.Workbench.Channel)
	value.AccountCenter.Channel = channel(value.AccountCenter.Channel)
	if !templateName.MatchString(value.RuntimeEngine.Family) || strings.TrimSpace(value.RuntimeEngine.EngineContract) == "" {
		return PlatformProfile{}, fmt.Errorf("Frontend Runtime Engine family 或 engineContract 无效")
	}
	if err := ValidateRenderAdapterConfig(value.RenderAdapter.Config); err != nil {
		return PlatformProfile{}, err
	}
	if err := ValidateShellConfig(value.Shell.Config); err != nil {
		return PlatformProfile{}, err
	}
	if value.Localization != nil && !containsFold(value.Localization.SupportedLocales, value.Localization.DefaultLocale) {
		return PlatformProfile{}, fmt.Errorf("Frontend Platform Profile 默认语言必须包含在 supportedLocales 中")
	}
	if value.Updates != nil && value.Updates.Mode != "refresh" && value.Updates.Mode != "notify" && value.Updates.Mode != "automatic" {
		return PlatformProfile{}, fmt.Errorf("Frontend Platform Profile updates.mode 无效: %s", value.Updates.Mode)
	}
	selectedFoundations := []PluginRef{value.RuntimeEngine.PluginRef, value.RenderAdapter.PluginRef, value.Shell.PluginRef, value.Workbench.PluginRef, *value.AccountCenter}
	foundationIDs := map[string]struct{}{}
	for _, selected := range selectedFoundations {
		if _, exists := foundationIDs[selected.ID]; exists {
			return PlatformProfile{}, fmt.Errorf("Runtime Engine、设计系统、Shell、Workbench 与个人中心必须由独立插件提供")
		}
		foundationIDs[selected.ID] = struct{}{}
	}
	found := map[string]bool{}
	for _, ref := range value.Plugins {
		for _, selected := range selectedFoundations {
			if same(ref, selected) {
				found[selected.ID] = true
			}
		}
	}
	if !found[value.RuntimeEngine.ID] || !found[value.RenderAdapter.ID] || !found[value.Shell.ID] || !found[value.Workbench.ID] || !found[value.AccountCenter.ID] {
		return PlatformProfile{}, fmt.Errorf("Frontend Platform Profile plugins 必须精确包含 Runtime Engine、设计系统、Shell、Workbench 与个人中心插件")
	}
	return value, nil
}

func ValidateRenderAdapterConfig(config RenderAdapterConfig) error {
	if !templateName.MatchString(config.DefaultRenderer) || len(config.AllowedRenderers) == 0 {
		return fmt.Errorf("渲染器默认值或允许目录无效")
	}
	allowed := map[string]struct{}{}
	for _, renderer := range config.AllowedRenderers {
		if !templateName.MatchString(renderer) {
			return fmt.Errorf("渲染器名称无效: %s", renderer)
		}
		if _, exists := allowed[renderer]; exists {
			return fmt.Errorf("渲染器目录重复: %s", renderer)
		}
		allowed[renderer] = struct{}{}
	}
	if _, ok := allowed[config.DefaultRenderer]; !ok {
		return fmt.Errorf("默认渲染器必须属于允许目录")
	}
	for renderer, options := range config.RendererOptions {
		if _, ok := allowed[renderer]; !ok || (options.ThemeTemplate != "" && !templateName.MatchString(options.ThemeTemplate)) ||
			(options.IconTheme != "" && !templateName.MatchString(options.IconTheme)) {
			return fmt.Errorf("渲染器选项无效: %s", renderer)
		}
		if err := validateSelectableRendererOptions(renderer, options.ThemeTemplate, options.AllowedThemeTemplates, options.ThemeUserSelectable, "主题模板"); err != nil {
			return err
		}
		if err := validateSelectableRendererOptions(renderer, options.IconTheme, options.AllowedIconThemes, options.IconUserSelectable, "图标主题"); err != nil {
			return err
		}
	}
	return nil
}

func validateSelectableRendererOptions(renderer, selected string, allowed []string, selectable bool, label string) error {
	seen := map[string]struct{}{}
	for _, value := range allowed {
		if !templateName.MatchString(value) {
			return fmt.Errorf("渲染器 %s 的%s目录无效", renderer, label)
		}
		if _, duplicate := seen[value]; duplicate {
			return fmt.Errorf("渲染器 %s 的%s目录重复", renderer, label)
		}
		seen[value] = struct{}{}
	}
	if selectable && len(allowed) == 0 {
		return fmt.Errorf("渲染器 %s 允许用户切换%s时必须提供允许目录", renderer, label)
	}
	if selected != "" && len(allowed) > 0 {
		if _, ok := seen[selected]; !ok {
			return fmt.Errorf("渲染器 %s 的默认%s必须属于允许目录", renderer, label)
		}
	}
	return nil
}

func ValidateNavigationConfig(config NavigationConfig) error {
	groups := map[string]NavigationGroupDescriptor{
		"primary":   {ID: "primary", Zone: "primary"},
		"secondary": {ID: "secondary", Zone: "secondary"},
		"settings":  {ID: "settings", Zone: "settings"},
	}
	configured := map[string]struct{}{}
	for _, group := range config.NavigationGroups {
		if !managementName.MatchString(group.ID) || strings.TrimSpace(group.Label) == "" {
			return fmt.Errorf("导航分组 id 或 label 无效: %s", group.ID)
		}
		if _, duplicate := configured[group.ID]; duplicate {
			return fmt.Errorf("导航分组 id 重复: %s", group.ID)
		}
		configured[group.ID] = struct{}{}
		if previous, builtin := groups[group.ID]; builtin && (group.ParentID != "" || group.Zone != previous.Zone) {
			return fmt.Errorf("内建导航分组不能跨语义区或改为子组: %s", group.ID)
		}
		groups[group.ID] = group
	}
	for _, group := range config.NavigationGroups {
		if group.ParentID == "" {
			continue
		}
		if group.ParentID == group.ID {
			return fmt.Errorf("导航分组不能引用自身: %s", group.ID)
		}
		parent, ok := groups[group.ParentID]
		if !ok {
			return fmt.Errorf("导航子组引用了未知根组: %s/%s", group.ID, group.ParentID)
		}
		if parent.ParentID != "" {
			return fmt.Errorf("导航深度超过 root group → child group → page: %s", group.ID)
		}
		if parent.Zone != group.Zone {
			return fmt.Errorf("导航子组不能跨语义区: %s/%s", group.ID, group.ParentID)
		}
	}
	placements := map[string]struct{}{}
	for _, placement := range config.NavigationPlacements {
		if !managementName.MatchString(placement.SemanticID) || !managementName.MatchString(placement.GroupID) {
			return fmt.Errorf("导航语义映射 id 无效: %s/%s", placement.SemanticID, placement.GroupID)
		}
		if _, duplicate := placements[placement.SemanticID]; duplicate {
			return fmt.Errorf("导航语义映射重复: %s", placement.SemanticID)
		}
		placements[placement.SemanticID] = struct{}{}
		if _, ok := groups[placement.GroupID]; !ok {
			return fmt.Errorf("导航语义映射引用了未知分组: %s/%s", placement.SemanticID, placement.GroupID)
		}
	}
	return nil
}

func ValidateShellConfig(config ShellConfig) error {
	if err := ValidateNavigationConfig(config.NavigationConfig); err != nil {
		return err
	}
	if !templateName.MatchString(config.DefaultTemplate) {
		return fmt.Errorf("Shell 默认模板无效: %s", config.DefaultTemplate)
	}
	if len(config.AllowedTemplates) == 0 {
		return fmt.Errorf("Shell 至少需要一个允许模板")
	}
	allowed := map[string]struct{}{}
	for _, template := range config.AllowedTemplates {
		if !templateName.MatchString(template) {
			return fmt.Errorf("Shell 模板无效: %s", template)
		}
		if _, exists := allowed[template]; exists {
			return fmt.Errorf("Shell 模板重复: %s", template)
		}
		allowed[template] = struct{}{}
	}
	if _, exists := allowed[config.DefaultTemplate]; !exists {
		return fmt.Errorf("Shell 默认模板必须包含在 allowedTemplates: %s", config.DefaultTemplate)
	}
	for template, options := range config.TemplateOptions {
		if _, exists := allowed[template]; !exists {
			return fmt.Errorf("Shell templateOptions 不能包含未允许模板: %s", template)
		}
		if options == nil {
			return fmt.Errorf("Shell 模板选项必须是对象: %s", template)
		}
	}
	return nil
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}
