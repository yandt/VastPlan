package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"

	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
)

// seedArtifactSelection is the exact immutable artifact closure required to
// start the local platform-management Seed. Application and example plugins
// deliberately stay outside this plan and enter services through the
// workspace/testing publication workflows.
type seedArtifactSelection struct {
	refs []artifactrepository.Ref
	byID map[string]artifactrepository.Ref
}

func loadSeedArtifactSelection(root string) (seedArtifactSelection, error) {
	backendRaw, err := os.ReadFile(filepath.Join(root, "engineering", "deploy", "platform-management-profile.json"))
	if err != nil {
		return seedArtifactSelection{}, err
	}
	var backend struct {
		Services []struct {
			Plugins []struct {
				ID, Version, Channel string
			} `json:"plugins"`
		} `json:"services"`
	}
	if err := json.Unmarshal(backendRaw, &backend); err != nil {
		return seedArtifactSelection{}, fmt.Errorf("解析 Seed Backend Platform Profile: %w", err)
	}
	portal, err := frontendcompositionv1.ParsePortalPlatformCatalogFile(
		filepath.Join(root, "engineering", "deploy", "portal-platform-catalog.json"),
	)
	if err != nil {
		return seedArtifactSelection{}, err
	}

	selection := seedArtifactSelection{byID: map[string]artifactrepository.Ref{}}
	add := func(id, version, channel, source string) error {
		ref := artifactrepository.Ref{
			PluginID: strings.TrimSpace(id), Version: strings.TrimSpace(version), Channel: strings.TrimSpace(channel),
		}
		if ref.Channel == "" {
			ref.Channel = "stable"
		}
		if ref.PluginID == "" || ref.Version == "" {
			return fmt.Errorf("%s 包含不完整的 Seed 插件引用", source)
		}
		if ref.Channel != "stable" {
			return fmt.Errorf("%s 的 Seed 插件 %s 必须使用 stable，实际为 %s", source, ref.PluginID, ref.Channel)
		}
		if previous, ok := selection.byID[ref.PluginID]; ok {
			if previous != ref {
				return fmt.Errorf("Seed 插件 %s 存在冲突引用: %s/%s 与 %s/%s", ref.PluginID, previous.Version, previous.Channel, ref.Version, ref.Channel)
			}
			return nil
		}
		selection.byID[ref.PluginID] = ref
		return nil
	}
	for _, service := range backend.Services {
		for _, ref := range service.Plugins {
			if err := add(ref.ID, ref.Version, ref.Channel, "Backend Platform Profile"); err != nil {
				return seedArtifactSelection{}, err
			}
		}
	}
	for _, profile := range portal.Profiles {
		platformRefs := []frontendcompositionv1.PluginRef{
			profile.RuntimeEngine.PluginRef, profile.RenderAdapter.PluginRef, profile.Shell.PluginRef, profile.Workbench.PluginRef,
		}
		platformRefs = append(platformRefs, profile.Plugins...)
		for _, ref := range platformRefs {
			if err := add(ref.ID, ref.Version, ref.Channel, "Portal Platform Catalog"); err != nil {
				return seedArtifactSelection{}, err
			}
		}
	}
	if len(selection.byID) == 0 {
		return seedArtifactSelection{}, fmt.Errorf("平台 Seed 配置没有引用任何插件")
	}

	// Manifest dependencies are development-time facts. Close them here so the
	// minimal Seed remains complete without an orchestration-time allow-list.
	queue := make([]string, 0, len(selection.byID))
	for id := range selection.byID {
		queue = append(queue, id)
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		manifest, err := readLocalPluginManifest(root, id)
		if err != nil {
			return seedArtifactSelection{}, err
		}
		selected := selection.byID[id]
		if manifest.Version != selected.Version {
			return seedArtifactSelection{}, fmt.Errorf("Seed 引用 %s@%s 与本地 Manifest %s 不一致", id, selected.Version, manifest.Version)
		}
		for dependencyID, rawConstraint := range manifest.Dependencies {
			dependency, err := readLocalPluginManifest(root, dependencyID)
			if err != nil {
				return seedArtifactSelection{}, fmt.Errorf("解析 %s 的依赖 %s: %w", id, dependencyID, err)
			}
			constraint, constraintErr := semver.NewConstraint(rawConstraint)
			version, versionErr := semver.NewVersion(dependency.Version)
			if constraintErr != nil || versionErr != nil || !constraint.Check(version) {
				return seedArtifactSelection{}, fmt.Errorf("Seed 依赖 %s 要求 %s@%s，本地版本为 %s", id, dependencyID, rawConstraint, dependency.Version)
			}
			dependencyRef := artifactrepository.Ref{PluginID: dependencyID, Version: dependency.Version, Channel: "stable"}
			if previous, exists := selection.byID[dependencyID]; exists {
				if previous != dependencyRef {
					return seedArtifactSelection{}, fmt.Errorf("Seed 依赖 %s 与已选择引用冲突", dependencyID)
				}
				continue
			}
			selection.byID[dependencyID] = dependencyRef
			queue = append(queue, dependencyID)
		}
	}
	selection.refs = make([]artifactrepository.Ref, 0, len(selection.byID))
	for _, ref := range selection.byID {
		selection.refs = append(selection.refs, ref)
	}
	sortArtifactRefs(selection.refs)
	return selection, nil
}

func readLocalPluginManifest(root, pluginID string) (pluginv1.Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(root, "extensions", "plugins", pluginID, "vastplan.plugin.json"))
	if err != nil {
		return pluginv1.Manifest{}, fmt.Errorf("读取插件 %s Manifest: %w", pluginID, err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		return pluginv1.Manifest{}, fmt.Errorf("解析插件 %s Manifest: %w", pluginID, err)
	}
	if manifest.ID != pluginID {
		return pluginv1.Manifest{}, fmt.Errorf("插件目录 %s 与 Manifest id %s 不一致", pluginID, manifest.ID)
	}
	return manifest, nil
}

func (s seedArtifactSelection) contains(pluginID string) bool {
	_, ok := s.byID[pluginID]
	return ok
}

func (s seedArtifactSelection) references() []artifactrepository.Ref {
	return append([]artifactrepository.Ref(nil), s.refs...)
}

func (s seedArtifactSelection) pluginIDs() []string {
	ids := make([]string, 0, len(s.refs))
	for _, ref := range s.refs {
		ids = append(ids, ref.PluginID)
	}
	return ids
}

func sortArtifactRefs(refs []artifactrepository.Ref) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].PluginID != refs[j].PluginID {
			return refs[i].PluginID < refs[j].PluginID
		}
		if refs[i].Version != refs[j].Version {
			return refs[i].Version < refs[j].Version
		}
		return refs[i].Channel < refs[j].Channel
	})
}

func validateExactSeedRefs(stage string, expected, actual []artifactrepository.Ref) error {
	if len(actual) != len(expected) {
		return fmt.Errorf("%s Seed 制品数量漂移: expected=%d actual=%d", stage, len(expected), len(actual))
	}
	for index := range expected {
		if actual[index] != expected[index] {
			return fmt.Errorf("%s Seed 制品引用漂移: expected=%s@%s/%s actual=%s@%s/%s", stage,
				expected[index].PluginID, expected[index].Version, expected[index].Channel,
				actual[index].PluginID, actual[index].Version, actual[index].Channel)
		}
	}
	return nil
}

func (r *runtime) seedSelection() (seedArtifactSelection, error) {
	if len(r.seedArtifacts.refs) > 0 {
		return r.seedArtifacts, nil
	}
	selection, err := loadSeedArtifactSelection(r.options.root)
	if err != nil {
		return seedArtifactSelection{}, err
	}
	r.seedArtifacts = selection
	return selection, nil
}

func (r *runtime) seedPackageSpecs() ([]packageSpec, error) {
	selection, err := r.seedSelection()
	if err != nil {
		return nil, err
	}
	all, err := discoverPackageSpecs(r.options.root)
	if err != nil {
		return nil, err
	}
	selected := make([]packageSpec, 0, len(selection.refs))
	covered := map[string]struct{}{
		"cn.vastplan.foundation.security.bootstrap-policy": {}, // packaged by the dynamic-go builder
	}
	for _, spec := range all {
		if selection.contains(spec.id) {
			selected = append(selected, spec)
			covered[spec.id] = struct{}{}
		}
	}
	for _, ref := range selection.refs {
		if _, ok := covered[ref.PluginID]; !ok {
			return nil, fmt.Errorf("Seed 插件 %s 没有可打包的执行或前端入口", ref.PluginID)
		}
	}
	return selected, nil
}

func frontendPluginIDs(specs []packageSpec) []string {
	ids := make([]string, 0, len(specs))
	for _, spec := range specs {
		if spec.frontend {
			ids = append(ids, spec.id)
		}
	}
	return ids
}
