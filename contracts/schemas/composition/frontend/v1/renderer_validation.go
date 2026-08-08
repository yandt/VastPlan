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
	if err := ValidateNavigationLocales(value.Shell.Config.NavigationOverrides, value.Localization); err != nil {
		return PlatformProfile{}, err
	}
	if err := ValidateNavigationFolderLocales(value.Shell.Config.NavigationFolders, value.Localization); err != nil {
		return PlatformProfile{}, err
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
	seen := map[string]struct{}{}
	for _, override := range config.NavigationOverrides {
		if !navigationGlobalID(override.Target) || (override.Parent != "" && !navigationGlobalID(override.Parent)) {
			return fmt.Errorf("导航覆盖 target 或 parent 无效: %s/%s", override.Target, override.Parent)
		}
		if override.Parent == override.Target {
			return fmt.Errorf("导航覆盖不能把节点挂到自身: %s", override.Target)
		}
		if _, duplicate := seen[override.Target]; duplicate {
			return fmt.Errorf("导航覆盖 target 重复: %s", override.Target)
		}
		seen[override.Target] = struct{}{}
		for locale, label := range override.Labels {
			if !localeName(locale) || strings.TrimSpace(label) == "" || len([]rune(label)) > 80 {
				return fmt.Errorf("导航覆盖语言或名称无效: %s/%s", override.Target, locale)
			}
		}
	}
	folderIDs, members := map[string]struct{}{}, map[string]struct{}{}
	for _, folder := range config.NavigationFolders {
		key := folder.ServiceID + "/" + folder.ID
		if !managementName.MatchString(folder.ID) || !managementName.MatchString(folder.ServiceID) || strings.TrimSpace(folder.Label) == "" || len([]rune(folder.Label)) > 80 || len(folder.Members) < 2 || len(folder.Members) > 64 {
			return fmt.Errorf("导航文件夹无效: %s", key)
		}
		if _, duplicate := folderIDs[key]; duplicate {
			return fmt.Errorf("导航文件夹重复: %s", key)
		}
		folderIDs[key] = struct{}{}
		seenMembers := map[string]struct{}{}
		for _, member := range folder.Members {
			if !navigationGlobalID(member) {
				return fmt.Errorf("导航文件夹成员无效: %s/%s", key, member)
			}
			if _, duplicate := seenMembers[member]; duplicate {
				return fmt.Errorf("导航文件夹成员重复: %s/%s", key, member)
			}
			seenMembers[member] = struct{}{}
			scoped := folder.ServiceID + "\x00" + member
			if _, duplicate := members[scoped]; duplicate {
				return fmt.Errorf("导航 root 在同一服务被重复收纳: %s/%s", folder.ServiceID, member)
			}
			members[scoped] = struct{}{}
		}
		for locale, label := range folder.Labels {
			if !localeName(locale) || strings.TrimSpace(label) == "" || len([]rune(label)) > 80 {
				return fmt.Errorf("导航文件夹语言或名称无效: %s/%s", key, locale)
			}
		}
		if folder.Icon != nil && (folder.Icon.Kind != "semantic" || !managementName.MatchString(folder.Icon.Name)) {
			return fmt.Errorf("导航文件夹图标无效: %s", key)
		}
	}
	return nil
}

func ValidateNavigationLocales(overrides []NavigationOverride, policy *LocalizationPolicy) error {
	for _, override := range overrides {
		for locale := range override.Labels {
			if policy == nil || !containsFold(policy.SupportedLocales, locale) {
				return fmt.Errorf("导航覆盖语言不属于 Portal supportedLocales: %s/%s", override.Target, locale)
			}
		}
	}
	return nil
}

func ValidateNavigationFolderLocales(folders []NavigationFolder, policy *LocalizationPolicy) error {
	for _, folder := range folders {
		for locale := range folder.Labels {
			if policy == nil || !containsFold(policy.SupportedLocales, locale) {
				return fmt.Errorf("导航文件夹语言不属于 Portal supportedLocales: %s/%s", folder.ID, locale)
			}
		}
	}
	return nil
}

func navigationGlobalID(value string) bool {
	parts := strings.Split(value, "/")
	return len(parts) == 2 && managementName.MatchString(parts[0]) && managementName.MatchString(parts[1])
}

func localeName(value string) bool {
	parts := strings.Split(value, "-")
	if len(parts) == 0 || len(parts[0]) < 2 || len(parts[0]) > 8 {
		return false
	}
	for _, part := range parts {
		if len(part) == 0 || len(part) > 8 {
			return false
		}
		for _, character := range part {
			if character < '0' || (character > '9' && character < 'A') || (character > 'Z' && character < 'a') || character > 'z' {
				return false
			}
		}
	}
	return true
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
