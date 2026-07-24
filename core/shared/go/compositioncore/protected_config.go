package compositioncore

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

// MergeProtectedConfig 合并平台所有的公共基线与服务所有的配置。
// 服务可以增加新分支，但不能替换基线叶子；返回值与两个输入完全分离。
func MergeProtectedConfig(baseline, service map[string]any) (map[string]any, error) {
	merged, err := cloneConfigMap(baseline)
	if err != nil {
		return nil, err
	}
	if merged == nil {
		merged = map[string]any{}
	}
	overlay, err := cloneConfigMap(service)
	if err != nil {
		return nil, err
	}
	if err := mergeProtectedMap(merged, overlay, "config"); err != nil {
		return nil, err
	}
	if len(merged) == 0 {
		return nil, nil
	}
	return merged, nil
}

func mergeProtectedMap(target, overlay map[string]any, path string) error {
	keys := make([]string, 0, len(overlay))
	for key := range overlay {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		incoming := overlay[key]
		current, exists := target[key]
		if !exists {
			target[key] = incoming
			continue
		}
		currentMap, currentOK := current.(map[string]any)
		incomingMap, incomingOK := incoming.(map[string]any)
		if currentOK && incomingOK {
			if err := mergeProtectedMap(currentMap, incomingMap, path+"."+key); err != nil {
				return err
			}
			continue
		}
		if !reflect.DeepEqual(current, incoming) {
			return fmt.Errorf("服务配置不能覆盖公共基线字段 %s.%s", path, key)
		}
	}
	return nil
}

func cloneConfigMap(value map[string]any) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("编码配置: %w", err)
	}
	var cloned map[string]any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return nil, fmt.Errorf("复制配置: %w", err)
	}
	return cloned, nil
}
