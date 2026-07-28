package releaseorchestrator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	semver "github.com/Masterminds/semver/v3"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type WorkspacePlugin struct {
	ID       string
	Version  string
	Path     string
	Manifest pluginv1.Manifest
}

type PluginWorkspace struct {
	Plugins map[string]WorkspacePlugin
}

func LoadPluginWorkspace(repositoryRoot string) (PluginWorkspace, error) {
	workspace := PluginWorkspace{Plugins: map[string]WorkspacePlugin{}}
	for _, parent := range []string{"extensions/plugins", "examples/plugins"} {
		entries, err := os.ReadDir(filepath.Join(repositoryRoot, parent))
		if err != nil {
			return PluginWorkspace{}, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			relative := filepath.ToSlash(filepath.Join(parent, entry.Name()))
			raw, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(relative), "vastplan.plugin.json"))
			if err != nil {
				return PluginWorkspace{}, err
			}
			manifest, err := pluginv1.ParseManifest(raw)
			if err != nil {
				return PluginWorkspace{}, fmt.Errorf("解析 %s: %w", relative, err)
			}
			if manifest.ID != entry.Name() {
				return PluginWorkspace{}, fmt.Errorf("%s 的目录名与 Manifest id 不一致", relative)
			}
			if _, exists := workspace.Plugins[manifest.ID]; exists {
				return PluginWorkspace{}, fmt.Errorf("插件 ID 重复: %s", manifest.ID)
			}
			version, err := semver.StrictNewVersion(manifest.Version)
			if err != nil || version.Prerelease() != "" || version.Metadata() != "" {
				return PluginWorkspace{}, fmt.Errorf("插件 %s 必须使用稳定严格 SemVer 作为源码版本", manifest.ID)
			}
			workspace.Plugins[manifest.ID] = WorkspacePlugin{ID: manifest.ID, Version: manifest.Version, Path: relative, Manifest: manifest}
		}
	}
	if len(workspace.Plugins) == 0 {
		return PluginWorkspace{}, errors.New("工作区没有插件")
	}
	if err := workspace.validateDependencies(); err != nil {
		return PluginWorkspace{}, err
	}
	return workspace, nil
}

func (w PluginWorkspace) SortedIDs() []string {
	ids := make([]string, 0, len(w.Plugins))
	for id := range w.Plugins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (w PluginWorkspace) ReverseDependencies(pluginID string) []string {
	var result []string
	for id, plugin := range w.Plugins {
		if _, ok := plugin.Manifest.Dependencies[pluginID]; ok {
			result = append(result, id)
		}
		for _, feature := range compositionFeatures(plugin.Manifest) {
			if _, ok := feature.Dependencies[pluginID]; ok {
				result = append(result, id)
				break
			}
		}
	}
	sort.Strings(result)
	return compactStrings(result)
}

func (w PluginWorkspace) validateDependencies() error {
	for id, plugin := range w.Plugins {
		for dependencyID, constraintText := range allDependencies(plugin.Manifest) {
			dependency, ok := w.Plugins[dependencyID]
			if !ok {
				return fmt.Errorf("插件 %s 依赖工作区中不存在的 %s", id, dependencyID)
			}
			constraint, err := semver.NewConstraint(constraintText)
			version, versionErr := semver.StrictNewVersion(dependency.Version)
			if err != nil || versionErr != nil || !constraint.Check(version) {
				return fmt.Errorf("插件 %s 的依赖 %s@%s 不接受工作区版本 %s", id, dependencyID, constraintText, dependency.Version)
			}
		}
	}
	state := map[string]uint8{}
	stack := make([]string, 0, len(w.Plugins))
	var visit func(string) error
	visit = func(id string) error {
		switch state[id] {
		case 1:
			return fmt.Errorf("插件依赖形成循环: %s -> %s", strings.Join(stack, " -> "), id)
		case 2:
			return nil
		}
		state[id] = 1
		stack = append(stack, id)
		dependencies := make([]string, 0, len(allDependencies(w.Plugins[id].Manifest)))
		for dependencyID := range allDependencies(w.Plugins[id].Manifest) {
			dependencies = append(dependencies, dependencyID)
		}
		sort.Strings(dependencies)
		for _, dependencyID := range dependencies {
			if err := visit(dependencyID); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = 2
		return nil
	}
	for _, id := range w.SortedIDs() {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func allDependencies(manifest pluginv1.Manifest) map[string]string {
	dependencies := make(map[string]string, len(manifest.Dependencies))
	for id, constraint := range manifest.Dependencies {
		dependencies[id] = constraint
	}
	for _, feature := range compositionFeatures(manifest) {
		for id, constraint := range feature.Dependencies {
			if previous, exists := dependencies[id]; exists && previous != constraint {
				dependencies[id] = previous + ", " + constraint
				continue
			}
			dependencies[id] = constraint
		}
	}
	return dependencies
}

func compositionFeatures(manifest pluginv1.Manifest) []pluginv1.CompositionFeature {
	if manifest.Composition == nil {
		return nil
	}
	return manifest.Composition.Features
}

func compactStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}
