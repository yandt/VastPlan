package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"cdsoft.com.cn/VastPlan/engineering/internal/plugindev"
)

func backendPluginDevelopmentStatus(stateRoot string) []plugindev.Status {
	paths, err := filepath.Glob(filepath.Join(stateRoot, "plugin-dev", "status", "*.json"))
	if err != nil {
		return nil
	}
	sort.Strings(paths)
	if len(paths) > 100 {
		paths = paths[len(paths)-100:]
	}
	values := make([]plugindev.Status, 0, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var value plugindev.Status
		if json.Unmarshal(raw, &value) != nil || value.SchemaVersion != 1 || value.PluginID == "" || value.UpdatedAt.IsZero() {
			continue
		}
		values = append(values, value)
	}
	return values
}
