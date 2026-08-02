package portaltrust

import (
	"encoding/json"
	"errors"
	"fmt"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

const (
	rendererModuleContributionKind = "frontend.rendererModules"
	shellLibraryContributionKind   = "frontend.shellLibraries"
)

type rendererModuleDescriptor struct {
	ID           string `json:"id"`
	Title        string `json:"title,omitempty"`
	Adapter      string `json:"adapter"`
	UIContract   string `json:"uiContract"`
	EngineFamily string `json:"engineFamily"`
	Framework    string `json:"framework"`
}

type shellLibraryDescriptor struct {
	ID         string `json:"id"`
	Title      string `json:"title,omitempty"`
	Shell      string `json:"shell"`
	UIContract string `json:"uiContract"`
}

func projectPortalContributionIndex(spec portalapi.PortalSpec, verified []verifiedPortalPlugin) (pluginv1.ContributionIndexSnapshot, error) {
	sourceDigest, err := portalSpecDigest(spec)
	if err != nil {
		return pluginv1.ContributionIndexSnapshot{}, fmt.Errorf("计算 Portal Inventory 来源摘要: %w", err)
	}
	values := make([]pluginv1.VerifiedArtifactManifest, 0, len(verified))
	for _, plugin := range verified {
		values = append(values, pluginv1.VerifiedArtifactManifest{Artifact: plugin.artifact, Manifest: plugin.manifest})
	}
	inventory, err := pluginv1.BuildPluginInventory(spec.Revision, sourceDigest, values)
	if err != nil {
		return pluginv1.ContributionIndexSnapshot{}, fmt.Errorf("生成 Portal Plugin Inventory: %w", err)
	}
	index, err := pluginv1.BuildContributionIndex(inventory, values)
	if err != nil {
		return pluginv1.ContributionIndexSnapshot{}, fmt.Errorf("生成 Portal Contribution Index: %w", err)
	}
	if err := validatePortalFoundationCandidates(spec, index); err != nil {
		return pluginv1.ContributionIndexSnapshot{}, err
	}
	return index, nil
}

func validatePortalFoundationCandidates(spec portalapi.PortalSpec, index pluginv1.ContributionIndexSnapshot) error {
	renderers := map[string]pluginv1.IndexedContribution{}
	shellLibraries := map[string]pluginv1.IndexedContribution{}
	for _, contribution := range index.Contributions {
		switch contribution.Kind {
		case rendererModuleContributionKind:
			var descriptor rendererModuleDescriptor
			if err := json.Unmarshal(contribution.Descriptor, &descriptor); err != nil || descriptor.ID != contribution.ID || descriptor.Adapter != "ui.render.adapter" || descriptor.UIContract != spec.RenderAdapter.UIContract || descriptor.EngineFamily != spec.RuntimeEngine.Family || descriptor.Framework == "" {
				return fmt.Errorf("Renderer Module Contribution 无效: %s/%s", contribution.Owner.Ref.PluginID, contribution.ID)
			}
			if err := requirePlatformProfileOwner(spec, contribution); err != nil {
				return err
			}
			if _, duplicate := renderers[descriptor.ID]; duplicate {
				return fmt.Errorf("Renderer ID 存在多个候选且未消歧: %s", descriptor.ID)
			}
			renderers[descriptor.ID] = contribution
		case shellLibraryContributionKind:
			var descriptor shellLibraryDescriptor
			if err := json.Unmarshal(contribution.Descriptor, &descriptor); err != nil || descriptor.ID != contribution.ID || descriptor.Shell != "ui.structure.shell" || descriptor.UIContract != spec.Shell.UIContract {
				return fmt.Errorf("Shell Library Contribution 无效: %s/%s", contribution.Owner.Ref.PluginID, contribution.ID)
			}
			if err := requirePlatformProfileOwner(spec, contribution); err != nil {
				return err
			}
			if _, duplicate := shellLibraries[descriptor.ID]; duplicate {
				return fmt.Errorf("Shell Library ID 存在多个候选且未消歧: %s", descriptor.ID)
			}
			shellLibraries[descriptor.ID] = contribution
		}
	}
	for _, id := range spec.RenderAdapter.Config.AllowedRenderers {
		if _, exists := renderers[id]; !exists {
			return fmt.Errorf("Platform Profile 允许了 Contribution Index 中不存在的 Renderer: %s", id)
		}
	}
	for _, id := range spec.Shell.Config.AllowedTemplates {
		if _, exists := shellLibraries[id]; !exists {
			return fmt.Errorf("Platform Profile 允许了 Contribution Index 中不存在的 Shell Library: %s", id)
		}
	}
	if _, exists := renderers[spec.RenderAdapter.Config.DefaultRenderer]; !exists {
		return errors.New("Portal 默认 Renderer 不在 Contribution Index 中")
	}
	if _, exists := shellLibraries[spec.Shell.Config.DefaultTemplate]; !exists {
		return errors.New("Portal 默认 Shell Library 不在 Contribution Index 中")
	}
	return validateNavigationCandidates(spec, index)
}

func requirePlatformProfileOwner(spec portalapi.PortalSpec, contribution pluginv1.IndexedContribution) error {
	ref := portalapi.PluginRef{ID: contribution.Owner.Ref.PluginID, Version: contribution.Owner.Ref.Version, Channel: contribution.Owner.Ref.Channel}
	if !containsPortalRef(spec.Plugins, ref) || spec.Resolution.PluginOrigins[ref.ID] != compositioncommonv1.OriginPlatformProfile {
		return fmt.Errorf("Foundation Library 未由 Platform Profile 精确锁定: %s/%s", contribution.Kind, contribution.ID)
	}
	return nil
}
