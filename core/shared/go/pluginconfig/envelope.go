// Package pluginconfig defines the service-unit configuration envelope shared by
// the control plane, Node Agent and Backend Kernel. Runtime operational fields
// and plugin-owned values are deliberately separated so a plugin can never read
// another plugin's configuration merely because both run in the same service.
package pluginconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

const (
	PluginsKey              = "plugins"
	EnvironmentAllowlistKey = "environment_allowlist"
	PartitionKeysKey        = "partition_keys"
)

// Envelope is the normalized, immutable view of ServiceUnit.config.
type Envelope struct {
	Plugins              map[string]map[string]any
	EnvironmentAllowlist map[string][]string
	PartitionKeys        []string
}

// Parse validates the configuration envelope against the plugins installed in
// the service unit. Unknown plugin IDs fail closed instead of silently creating
// configuration or environment grants for code that is not part of the unit.
func Parse(config map[string]any, installedPluginIDs []string) (Envelope, error) {
	envelope := Envelope{
		Plugins:              map[string]map[string]any{},
		EnvironmentAllowlist: map[string][]string{},
	}
	installed := make(map[string]struct{}, len(installedPluginIDs))
	for _, id := range installedPluginIDs {
		if id == "" {
			return Envelope{}, errors.New("插件配置作用域包含空 plugin id")
		}
		installed[id] = struct{}{}
	}
	if config == nil {
		return envelope, nil
	}
	for key := range config {
		switch key {
		case PluginsKey, EnvironmentAllowlistKey, PartitionKeysKey:
		default:
			return Envelope{}, fmt.Errorf("service config 包含未知顶层字段 %q", key)
		}
	}
	if raw, ok := config[PluginsKey]; ok {
		values, err := object(raw)
		if err != nil {
			return Envelope{}, fmt.Errorf("service config.%s: %w", PluginsKey, err)
		}
		for pluginID, rawConfig := range values {
			if _, ok := installed[pluginID]; !ok {
				return Envelope{}, fmt.Errorf("service config 为未安装插件 %q 提供配置", pluginID)
			}
			pluginValues, err := object(rawConfig)
			if err != nil {
				return Envelope{}, fmt.Errorf("插件 %q 配置: %w", pluginID, err)
			}
			envelope.Plugins[pluginID] = cloneObject(pluginValues)
		}
	}
	if raw, ok := config[EnvironmentAllowlistKey]; ok {
		values, err := object(raw)
		if err != nil {
			return Envelope{}, fmt.Errorf("service config.%s: %w", EnvironmentAllowlistKey, err)
		}
		for pluginID, rawNames := range values {
			if _, ok := installed[pluginID]; !ok {
				return Envelope{}, fmt.Errorf("service config 为未安装插件 %q 授予环境变量", pluginID)
			}
			names, err := stringList(rawNames)
			if err != nil {
				return Envelope{}, fmt.Errorf("插件 %q environment_allowlist: %w", pluginID, err)
			}
			envelope.EnvironmentAllowlist[pluginID] = names
		}
	}
	if raw, ok := config[PartitionKeysKey]; ok {
		keys, err := stringList(raw)
		if err != nil {
			return Envelope{}, fmt.Errorf("service config.%s: %w", PartitionKeysKey, err)
		}
		envelope.PartitionKeys = keys
	}
	return envelope, nil
}

func object(value any) (map[string]any, error) {
	if typed, ok := value.(map[string]any); ok {
		return typed, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("必须是对象")
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded == nil {
		return nil, errors.New("必须是对象")
	}
	return decoded, nil
}

func stringList(value any) ([]string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("必须是字符串数组")
	}
	var decoded []string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, errors.New("必须是字符串数组")
	}
	seen := make(map[string]struct{}, len(decoded))
	for _, item := range decoded {
		if item == "" {
			return nil, errors.New("不能包含空字符串")
		}
		if _, exists := seen[item]; exists {
			return nil, fmt.Errorf("包含重复值 %q", item)
		}
		seen[item] = struct{}{}
	}
	sort.Strings(decoded)
	return decoded, nil
}

func cloneObject(input map[string]any) map[string]any {
	raw, _ := json.Marshal(input)
	var output map[string]any
	_ = json.Unmarshal(raw, &output)
	return output
}
