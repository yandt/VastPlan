//go:build (linux || darwin || freebsd) && cgo

package loader

import (
	"debug/buildinfo"
	"errors"
	"fmt"
	"plugin"
	"runtime/debug"
	"strings"

	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

func load(filename, pluginID, version, hostFingerprint string) (definition protocolbus.EmbeddedPlugin, err error) {
	if strings.TrimSpace(filename) == "" {
		return definition, errors.New("dynamic-go 入口路径不能为空")
	}
	if err := validateBuildInfo(filename); err != nil {
		return definition, err
	}
	loaded, err := plugin.Open(filename)
	if err != nil {
		return definition, fmt.Errorf("打开 dynamic-go 模块: %w", err)
	}
	symbol, err := loaded.Lookup(protocolbus.DynamicGoSymbol)
	if err != nil {
		return definition, fmt.Errorf("dynamic-go 缺少导出函数 %s: %w", protocolbus.DynamicGoSymbol, err)
	}
	entrypoint, ok := symbol.(func() protocolbus.DynamicGoModule)
	if !ok {
		return definition, fmt.Errorf("dynamic-go 导出 %s 的函数签名不兼容", protocolbus.DynamicGoSymbol)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("dynamic-go 入口 panic: %v", recovered)
		}
	}()
	module := entrypoint()
	if module.ABI != protocolbus.DynamicGoABIV1 {
		return definition, fmt.Errorf("dynamic-go ABI 不兼容: 期望 %s，实际 %s", protocolbus.DynamicGoABIV1, module.ABI)
	}
	if module.BuildFingerprint == "" || module.BuildFingerprint != hostFingerprint {
		return definition, fmt.Errorf("dynamic-go 构建指纹不一致: host=%s module=%s", hostFingerprint, module.BuildFingerprint)
	}
	if module.Plugin.ID != pluginID || module.Plugin.Version != version {
		return definition, fmt.Errorf("dynamic-go 模块身份与验签制品不一致: 期望 %s@%s，实际 %s@%s",
			pluginID, version, module.Plugin.ID, module.Plugin.Version)
	}
	return module.Plugin, nil
}

func validateBuildInfo(filename string) error {
	module, err := buildinfo.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("读取 dynamic-go 构建信息: %w", err)
	}
	host, ok := debug.ReadBuildInfo()
	if !ok {
		return errors.New("读取 Go Runtime Host 构建信息失败")
	}
	if module.GoVersion != host.GoVersion {
		return fmt.Errorf("dynamic-go Go 工具链不一致: host=%s module=%s", host.GoVersion, module.GoVersion)
	}
	if err := compareBuildSettings(host.Settings, module.Settings); err != nil {
		return err
	}
	hostDeps := dependencyMap(host)
	for path, moduleValue := range dependencyMap(module) {
		if path == host.Main.Path || path == module.Main.Path {
			continue
		}
		if hostValue, shared := hostDeps[path]; shared && hostValue != moduleValue {
			return fmt.Errorf("dynamic-go 公共依赖不一致: %s host=%s module=%s", path, hostValue, moduleValue)
		}
	}
	return nil
}

func compareBuildSettings(host, module []debug.BuildSetting) error {
	wanted := map[string]bool{
		"GOOS": true, "GOARCH": true, "CGO_ENABLED": true,
		"GOAMD64": true, "GOARM": true, "GOARM64": true,
		"-race": true, "-msan": true, "-asan": true,
	}
	values := func(settings []debug.BuildSetting) map[string]string {
		out := map[string]string{}
		for _, setting := range settings {
			if wanted[setting.Key] {
				out[setting.Key] = setting.Value
			}
		}
		return out
	}
	hostValues, moduleValues := values(host), values(module)
	for key := range wanted {
		if hostValues[key] != moduleValues[key] {
			return fmt.Errorf("dynamic-go 构建参数不一致: %s host=%q module=%q", key, hostValues[key], moduleValues[key])
		}
	}
	return nil
}

func dependencyMap(info *debug.BuildInfo) map[string]string {
	out := map[string]string{}
	if info.Main.Path != "" {
		out[info.Main.Path] = info.Main.Version + "@" + info.Main.Sum
	}
	for _, dependency := range info.Deps {
		value := dependency.Version + "@" + dependency.Sum
		if dependency.Replace != nil {
			value += "=>" + dependency.Replace.Path + "@" + dependency.Replace.Version + "@" + dependency.Replace.Sum
		}
		out[dependency.Path] = value
	}
	return out
}
