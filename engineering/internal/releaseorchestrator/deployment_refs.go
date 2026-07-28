package releaseorchestrator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type DeploymentReferenceChange struct {
	Path        string `json:"path"`
	PluginID    string `json:"pluginId"`
	FromVersion string `json:"fromVersion"`
	ToVersion   string `json:"toVersion"`
	Occurrences int    `json:"occurrences"`
}

func DeploymentReferenceChanges(repositoryRoot string, versions map[string]string) ([]DeploymentReferenceChange, error) {
	return syncDeploymentReferences(repositoryRoot, versions, false)
}

func SyncDeploymentReferences(repositoryRoot string, versions map[string]string) ([]DeploymentReferenceChange, error) {
	return syncDeploymentReferences(repositoryRoot, versions, true)
}

func syncDeploymentReferences(repositoryRoot string, versions map[string]string, write bool) ([]DeploymentReferenceChange, error) {
	if len(versions) == 0 {
		return nil, errors.New("同步部署引用至少需要一个插件版本")
	}
	files, err := filepath.Glob(filepath.Join(repositoryRoot, "engineering", "deploy", "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var changes []DeploymentReferenceChange
	for _, path := range files {
		relative, err := filepath.Rel(repositoryRoot, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("部署文件逃逸仓库边界: %s", path)
		}
		relative = filepath.ToSlash(relative)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var document any
		if err := json.Unmarshal(raw, &document); err != nil {
			return nil, fmt.Errorf("解析 %s: %w", path, err)
		}
		semantic := map[string]map[string]int{}
		collectDeploymentReferences(document, versions, semantic)
		if len(semantic) == 0 {
			continue
		}
		next := append([]byte(nil), raw...)
		ids := make([]string, 0, len(semantic))
		for id := range semantic {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			target := versions[id]
			pattern := regexp.MustCompile(`("id"\s*:\s*"` + regexp.QuoteMeta(id) + `"\s*,\s*"version"\s*:\s*")([^"]+)(")`)
			updated := 0
			next = pattern.ReplaceAllFunc(next, func(value []byte) []byte {
				match := pattern.FindSubmatch(value)
				if string(match[2]) == target {
					return value
				}
				updated++
				return append(append(append([]byte(nil), match[1]...), target...), match[3]...)
			})
			for sourceVersion, count := range semantic[id] {
				if sourceVersion == target {
					continue
				}
				changes = append(changes, DeploymentReferenceChange{
					Path: relative, PluginID: id,
					FromVersion: sourceVersion, ToVersion: target, Occurrences: count,
				})
			}
			if updated != changedReferenceCount(semantic[id], target) {
				return nil, fmt.Errorf("%s 中 %s 的 JSON 引用布局无法安全更新", path, id)
			}
		}
		if write && !bytes.Equal(raw, next) {
			if err := os.WriteFile(path, next, 0o644); err != nil {
				return nil, err
			}
		}
	}
	sort.Slice(changes, func(left, right int) bool {
		if changes[left].Path != changes[right].Path {
			return changes[left].Path < changes[right].Path
		}
		if changes[left].PluginID != changes[right].PluginID {
			return changes[left].PluginID < changes[right].PluginID
		}
		return changes[left].FromVersion < changes[right].FromVersion
	})
	return changes, nil
}

func collectDeploymentReferences(value any, versions map[string]string, found map[string]map[string]int) {
	switch typed := value.(type) {
	case map[string]any:
		id, idOK := typed["id"].(string)
		version, versionOK := typed["version"].(string)
		if idOK && versionOK {
			if _, selected := versions[id]; selected {
				if found[id] == nil {
					found[id] = map[string]int{}
				}
				found[id][version]++
			}
		}
		for _, child := range typed {
			collectDeploymentReferences(child, versions, found)
		}
	case []any:
		for _, child := range typed {
			collectDeploymentReferences(child, versions, found)
		}
	}
}

func changedReferenceCount(versions map[string]int, target string) int {
	count := 0
	for version, occurrences := range versions {
		if version != target {
			count += occurrences
		}
	}
	return count
}
