package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	semver "github.com/Masterminds/semver/v3"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/engineering/internal/pluginsbom"
)

type automaticSBOMInputs struct {
	Source, DependencySource, BackendBin, NodeBackendModule, FrontendGraphRoot, DynamicGoBin string
}

func generateAutomaticSBOM(inputs automaticSBOMInputs) (string, func(), error) {
	manifestRaw, err := os.ReadFile(filepath.Join(inputs.Source, "vastplan.plugin.json"))
	if err != nil {
		return "", func() {}, err
	}
	manifest, err := pluginv1.ParseManifest(manifestRaw)
	if err != nil {
		return "", func() {}, err
	}
	if err := validateSBOMDependencySource(manifest, inputs.DependencySource); err != nil {
		return "", func() {}, err
	}
	if manifest.SupplyChain != nil && manifest.SupplyChain.SBOM != nil {
		filename := filepath.Join(inputs.Source, filepath.FromSlash(manifest.SupplyChain.SBOM.Path))
		if info, err := os.Stat(filename); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return "", func() {}, errors.New("签名清单声明了 SBOM，但插件源码目录缺少对应普通文件")
		}
		return filename, func() {}, nil
	}
	workspaceRoot, err := findWorkspaceRoot(inputs.Source)
	if err != nil {
		return "", func() {}, err
	}
	goBinaries := nonempty(inputs.BackendBin, inputs.DynamicGoBin)
	driver := "native"
	if manifest.Execution != nil && manifest.Execution.Backend != nil && manifest.Execution.Backend.Driver != "" {
		driver = manifest.Execution.Backend.Driver
	}
	if len(goBinaries) == 0 && driver == "native" {
		candidate := filepath.Join(inputs.Source, filepath.FromSlash(manifest.Entry["backend"]))
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			goBinaries = append(goBinaries, candidate)
		}
	}
	metafiles := automaticMetafiles(inputs)
	dependencySource := inputs.DependencySource
	if dependencySource == "" {
		dependencySource = inputs.Source
	}
	result, err := pluginsbom.Generate(pluginsbom.Options{Root: workspaceRoot, PluginDir: inputs.Source, DependencyDir: dependencySource, GoBinaries: goBinaries, Metafiles: metafiles})
	if err != nil {
		return "", func() {}, fmt.Errorf("自动生成 %s SBOM: %w", manifest.ID, err)
	}
	directory, err := os.MkdirTemp("", "vastplan-plugin-sbom-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	filename := filepath.Join(directory, "sbom.cdx.json")
	if err := os.WriteFile(filename, result.Raw, 0o600); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return filename, cleanup, nil
}

func validateSBOMDependencySource(candidate pluginv1.Manifest, source string) error {
	if strings.TrimSpace(source) == "" {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(source, "vastplan.plugin.json"))
	if err != nil {
		return fmt.Errorf("读取 SBOM 依赖源 Manifest: %w", err)
	}
	original, err := pluginv1.ParseManifest(raw)
	if err != nil {
		return fmt.Errorf("解析 SBOM 依赖源 Manifest: %w", err)
	}
	candidateVersion, candidateErr := semver.StrictNewVersion(candidate.Version)
	originalVersion, originalErr := semver.StrictNewVersion(original.Version)
	if candidateErr != nil || originalErr != nil || originalVersion.Prerelease() != "" || originalVersion.Metadata() != "" ||
		candidate.ID != original.ID || candidate.Publisher != original.Publisher ||
		candidateVersion.Major() != originalVersion.Major() || candidateVersion.Minor() != originalVersion.Minor() || candidateVersion.Patch() != originalVersion.Patch() {
		return errors.New("SBOM 依赖源必须与候选使用相同插件身份、发布者和稳定基础版本")
	}
	return nil
}

func automaticMetafiles(inputs automaticSBOMInputs) []string {
	set := map[string]struct{}{}
	if inputs.NodeBackendModule != "" {
		set[filepath.Join(filepath.Dir(inputs.NodeBackendModule), "vastplan.node-metafile.json")] = struct{}{}
	}
	roots := []string{inputs.FrontendGraphRoot, inputs.Source}
	for _, root := range roots {
		if root == "" {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(root, "frontend", "dist", "vastplan.*-metafile.json"))
		for _, match := range matches {
			set[match] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for filename := range set {
		if info, err := os.Stat(filename); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			result = append(result, filename)
		}
	}
	sort.Strings(result)
	return result
}

func findWorkspaceRoot(source string) (string, error) {
	current, err := filepath.Abs(source)
	if err != nil {
		return "", err
	}
	for {
		if regularFile(filepath.Join(current, "go.mod")) && regularFile(filepath.Join(current, "pnpm-workspace.yaml")) {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("无法从插件目录定位 VastPlan 工作区根")
		}
		current = parent
	}
}

func regularFile(filename string) bool {
	info, err := os.Stat(filename)
	return err == nil && info.Mode().IsRegular()
}

func nonempty(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	return result
}
