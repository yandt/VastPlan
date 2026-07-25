// Package plugindev implements the development-only Backend plugin candidate
// controller. It never runs inside a kernel and never publishes on startup.
package plugindev

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	semver "github.com/Masterminds/semver/v3"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type Driver string

const (
	DriverNativeGo          Driver = "native-go"
	DriverDynamicGo         Driver = "dynamic-go"
	DriverNodeWorker        Driver = "node-worker"
	DriverPython            Driver = "python"
	DriverPythonInterpreter Driver = "python-subinterpreter"
)

type Spec struct {
	ID         string
	Version    string
	Entry      string
	Driver     Driver
	SourceRoot string
	Relative   string
	Manifest   pluginv1.Manifest
}

func Discover(repositoryRoot, selector string) (Spec, error) {
	repositoryRoot, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return Spec{}, err
	}
	pluginsRoot := filepath.Join(repositoryRoot, "extensions", "plugins")
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return Spec{}, errors.New("插件选择器不能为空")
	}
	candidate := selector
	if !filepath.IsAbs(candidate) {
		if strings.Contains(candidate, string(filepath.Separator)) || strings.HasPrefix(candidate, ".") {
			candidate = filepath.Join(repositoryRoot, candidate)
		} else {
			candidate = filepath.Join(pluginsRoot, candidate)
		}
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return Spec{}, err
	}
	realPlugins, err := filepath.EvalSymlinks(pluginsRoot)
	if err != nil {
		return Spec{}, fmt.Errorf("解析插件根目录: %w", err)
	}
	realCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return Spec{}, fmt.Errorf("解析插件目录: %w", err)
	}
	relativeToPlugins, err := filepath.Rel(realPlugins, realCandidate)
	if err != nil || relativeToPlugins == "." || strings.HasPrefix(relativeToPlugins, ".."+string(filepath.Separator)) || filepath.IsAbs(relativeToPlugins) {
		return Spec{}, errors.New("插件目录必须是 extensions/plugins 的直接子目录")
	}
	if strings.Contains(relativeToPlugins, string(filepath.Separator)) {
		return Spec{}, errors.New("插件选择器必须指向一个插件根目录")
	}
	raw, err := os.ReadFile(filepath.Join(realCandidate, "vastplan.plugin.json"))
	if err != nil {
		return Spec{}, fmt.Errorf("读取插件清单: %w", err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		return Spec{}, fmt.Errorf("解析插件清单: %w", err)
	}
	if manifest.ID != filepath.Base(realCandidate) {
		return Spec{}, errors.New("插件目录名与 Manifest ID 不一致")
	}
	version, err := semver.StrictNewVersion(manifest.Version)
	if err != nil || version.Prerelease() != "" || version.Metadata() != "" {
		return Spec{}, errors.New("workspace 开发要求源 Manifest 使用无预发布/metadata 的严格 SemVer")
	}
	entry := strings.TrimSpace(manifest.Entry["backend"])
	if entry == "" || strings.Contains(entry, "..") || filepath.IsAbs(filepath.FromSlash(entry)) {
		return Spec{}, errors.New("插件没有安全的 Backend 入口")
	}
	driver := DriverNativeGo
	if manifest.Execution != nil && manifest.Execution.Backend != nil {
		execution := manifest.Execution.Backend
		switch {
		case execution.DynamicGo != nil:
			driver = DriverDynamicGo
		case execution.Driver == "node-worker":
			driver = DriverNodeWorker
		case execution.Driver == "python":
			driver = DriverPython
		case execution.Driver == "python-subinterpreter":
			driver = DriverPythonInterpreter
		case execution.Driver == "" || execution.Driver == "native":
			driver = DriverNativeGo
		default:
			return Spec{}, fmt.Errorf("workspace-fast 暂不支持 Backend driver %q", execution.Driver)
		}
	}
	if driver == DriverNativeGo {
		if info, err := os.Stat(filepath.Join(realCandidate, "backend", "main.go")); err != nil || !info.Mode().IsRegular() {
			return Spec{}, errors.New("native-go 插件缺少 backend/main.go")
		}
	}
	relative, err := filepath.Rel(repositoryRoot, realCandidate)
	if err != nil {
		return Spec{}, err
	}
	return Spec{
		ID: manifest.ID, Version: manifest.Version, Entry: entry, Driver: driver,
		SourceRoot: realCandidate, Relative: filepath.ToSlash(relative), Manifest: manifest,
	}, nil
}

func WorkspaceVersion(baseVersion, sourceDigest string) (string, error) {
	version, err := semver.StrictNewVersion(baseVersion)
	if err != nil || version.Prerelease() != "" || version.Metadata() != "" {
		return "", errors.New("workspace 源版本必须是稳定严格 SemVer")
	}
	sourceDigest = strings.ToLower(strings.TrimSpace(sourceDigest))
	if len(sourceDigest) < 12 || len(sourceDigest) > 64 {
		return "", errors.New("workspace source digest 长度无效")
	}
	for _, value := range sourceDigest {
		if (value < '0' || value > '9') && (value < 'a' || value > 'f') {
			return "", errors.New("workspace source digest 必须是小写十六进制")
		}
	}
	return fmt.Sprintf("%d.%d.%d-dev.workspace.%s", version.Major(), version.Minor(), version.Patch(), sourceDigest), nil
}
