// Package pluginconfig defines the service-unit configuration envelope shared by
// the control plane, Node Agent and Backend Kernel. Runtime operational fields
// and plugin-owned values are deliberately separated so a plugin can never read
// another plugin's configuration merely because both run in the same service.
package pluginconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"cdsoft.com.cn/VastPlan/core/shared/go/protocol"
)

const (
	PluginsKey              = "plugins"
	ManagedCredentialsKey   = "managed_credentials"
	EnvironmentAllowlistKey = "environment_allowlist"
	PartitionKeysKey        = "partition_keys"
)

// Envelope is the normalized, immutable view of ServiceUnit.config.
type Envelope struct {
	Plugins              map[string]map[string]any
	ManagedCredentials   map[string]map[string]ManagedCredentialRef
	EnvironmentAllowlist map[string][]string
	PartitionKeys        []string
}

// Map returns a detached configuration envelope suitable for a new immutable
// Deployment revision. Credential references are non-secret but remain scoped
// under their owning plugin instead of being mixed into plugin values.
func (e Envelope) Map() map[string]any {
	out := map[string]any{}
	if len(e.Plugins) > 0 {
		out[PluginsKey] = cloneJSONValue(e.Plugins)
	}
	if len(e.ManagedCredentials) > 0 {
		out[ManagedCredentialsKey] = cloneJSONValue(e.ManagedCredentials)
	}
	if len(e.EnvironmentAllowlist) > 0 {
		out[EnvironmentAllowlistKey] = cloneJSONValue(e.EnvironmentAllowlist)
	}
	if len(e.PartitionKeys) > 0 {
		out[PartitionKeysKey] = append([]string(nil), e.PartitionKeys...)
	}
	return out
}

func cloneJSONValue[T any](value T) T {
	raw, _ := json.Marshal(value)
	var out T
	_ = json.Unmarshal(raw, &out)
	return out
}

// Parse validates the configuration envelope against the plugins installed in
// the service unit. Unknown plugin IDs fail closed instead of silently creating
// configuration or environment grants for code that is not part of the unit.
func Parse(config map[string]any, installedPluginIDs []string) (Envelope, error) {
	envelope := Envelope{
		Plugins:              map[string]map[string]any{},
		ManagedCredentials:   map[string]map[string]ManagedCredentialRef{},
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
		case PluginsKey, ManagedCredentialsKey, EnvironmentAllowlistKey, PartitionKeysKey:
		default:
			return Envelope{}, fmt.Errorf("service config 包含未知顶层字段 %q", key)
		}
	}
	if raw, ok := config[ManagedCredentialsKey]; ok {
		values, err := object(raw)
		if err != nil {
			return Envelope{}, fmt.Errorf("service config.%s: %w", ManagedCredentialsKey, err)
		}
		for pluginID, rawFields := range values {
			if _, ok := installed[pluginID]; !ok {
				return Envelope{}, fmt.Errorf("service config 为未安装插件 %q 提供托管凭证", pluginID)
			}
			fields, err := object(rawFields)
			if err != nil {
				return Envelope{}, fmt.Errorf("插件 %q 托管凭证: %w", pluginID, err)
			}
			envelope.ManagedCredentials[pluginID] = map[string]ManagedCredentialRef{}
			for fieldID, rawRef := range fields {
				ref, err := managedCredentialRef(rawRef)
				if err != nil || fieldID == "" || ref.Owner != pluginID {
					return Envelope{}, fmt.Errorf("插件 %q 托管凭证字段 %q 无效", pluginID, fieldID)
				}
				envelope.ManagedCredentials[pluginID][fieldID] = ref
			}
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
			frozen := cloneObject(pluginValues)
			raw, err := json.Marshal(frozen)
			if err != nil || len(raw) > protocol.MaxPluginConfigBytes {
				return Envelope{}, fmt.Errorf("插件 %q 配置超过 64KiB 上限", pluginID)
			}
			envelope.Plugins[pluginID] = frozen
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

func managedCredentialRef(value any) (ManagedCredentialRef, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return ManagedCredentialRef{}, err
	}
	var ref ManagedCredentialRef
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ref); err != nil || !strings.HasPrefix(ref.Handle, "credential://managed/") || ref.Scope != "tenant" || ref.Owner == "" || ref.Purpose == "" || ref.Version < 1 {
		return ManagedCredentialRef{}, errors.New("必须是有效托管凭证引用")
	}
	return ref, nil
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
