package releaseorchestrator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type packageVersionDocument struct {
	Version string `json:"version"`
}

// SyncSelectedPluginPackageVersions keeps language package metadata as a
// projection of the signed plugin Manifest. Only plugins selected by the
// release spec are touched, so an unrelated release cannot rewrite the whole
// workspace.
func SyncSelectedPluginPackageVersions(repositoryRoot string, workspace PluginWorkspace, selected map[string]string, write bool) ([]DerivedChange, error) {
	var changes []DerivedChange
	pluginIDs := make([]string, 0, len(selected))
	for pluginID := range selected {
		pluginIDs = append(pluginIDs, pluginID)
	}
	sort.Strings(pluginIDs)
	for _, pluginID := range pluginIDs {
		version := selected[pluginID]
		plugin, ok := workspace.Plugins[pluginID]
		if !ok {
			return nil, fmt.Errorf("package version 投影引用不存在的插件 %s", pluginID)
		}
		for _, face := range []string{"backend", "frontend", "runner", "mobile"} {
			relative := filepath.ToSlash(filepath.Join(plugin.Path, face, "package.json"))
			path := filepath.Join(repositoryRoot, filepath.FromSlash(relative))
			raw, err := os.ReadFile(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			var document packageVersionDocument
			if err := json.Unmarshal(raw, &document); err != nil {
				return nil, fmt.Errorf("解析 %s: %w", relative, err)
			}
			if document.Version == version {
				continue
			}
			expected, err := replaceJSONVersion(raw, version)
			if err != nil {
				return nil, fmt.Errorf("更新 %s: %w", relative, err)
			}
			changes = append(changes, DerivedChange{Path: relative, Reason: "signed manifest package version projection"})
			if write {
				if err := os.WriteFile(path, expected, 0o644); err != nil {
					return nil, err
				}
			}
		}
	}
	return changes, nil
}

func replaceJSONVersion(raw []byte, version string) ([]byte, error) {
	marker := []byte(`"version"`)
	start := bytes.Index(raw, marker)
	if start < 0 {
		return nil, errors.New("缺少 version 字段")
	}
	colon := bytes.IndexByte(raw[start+len(marker):], ':')
	if colon < 0 {
		return nil, errors.New("version 字段格式无效")
	}
	valueStart := start + len(marker) + colon + 1
	for valueStart < len(raw) && (raw[valueStart] == ' ' || raw[valueStart] == '\t') {
		valueStart++
	}
	if valueStart >= len(raw) || raw[valueStart] != '"' {
		return nil, errors.New("version 值必须是字符串")
	}
	valueEnd := valueStart + 1
	for valueEnd < len(raw) && raw[valueEnd] != '"' {
		valueEnd++
	}
	if valueEnd >= len(raw) {
		return nil, errors.New("version 字符串未结束")
	}
	out := make([]byte, 0, len(raw)+len(version))
	out = append(out, raw[:valueStart+1]...)
	out = append(out, version...)
	out = append(out, raw[valueEnd:]...)
	return out, nil
}
