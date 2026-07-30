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
	"strconv"
	"strings"

	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
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
	if write {
		if err := syncPortalProfileDigestReferences(repositoryRoot); err != nil {
			return nil, err
		}
	}
	return changes, nil
}

// syncPortalProfileDigestReferences closes the derived-reference chain after a
// plugin version changes a Frontend Platform Profile digest. Portal bindings
// and Access Profiles must move together; callers must never repair these
// locks manually after plugin-release prepare.
func syncPortalProfileDigestReferences(repositoryRoot string) error {
	catalogPath := filepath.Join(repositoryRoot, "engineering", "deploy", "portal-platform-catalog.json")
	raw, err := os.ReadFile(catalogPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var catalog frontendcompositionv1.PortalPlatformCatalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return fmt.Errorf("解析 Portal Platform Catalog: %w", err)
	}
	files, err := filepath.Glob(filepath.Join(repositoryRoot, "engineering", "deploy", "*.json"))
	if err != nil {
		return err
	}
	for _, profile := range catalog.Profiles {
		digest := profile.Digest()
		pattern := regexp.MustCompile(`("id"\s*:\s*"` + regexp.QuoteMeta(profile.ID) + `"\s*,\s*"revision"\s*:\s*` + strconv.FormatUint(profile.Revision, 10) + `\s*,\s*"digest"\s*:\s*")([a-f0-9]{64})(")`)
		for _, path := range files {
			value, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			var document any
			if err := json.Unmarshal(value, &document); err != nil {
				return fmt.Errorf("解析 %s: %w", path, err)
			}
			expected := countPortalProfileReferences(document, profile.ID, profile.Revision)
			updated := 0
			next := pattern.ReplaceAllFunc(value, func(match []byte) []byte {
				updated++
				parts := pattern.FindSubmatch(match)
				return append(append(append([]byte(nil), parts[1]...), digest...), parts[3]...)
			})
			if updated != expected {
				return fmt.Errorf("%s 中 Portal Profile %s@%d 的摘要锁布局无法安全更新", path, profile.ID, profile.Revision)
			}
			if !bytes.Equal(value, next) {
				if err := os.WriteFile(path, next, 0o644); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func countPortalProfileReferences(value any, profileID string, revision uint64) int {
	switch typed := value.(type) {
	case map[string]any:
		count := 0
		id, idOK := typed["id"].(string)
		digest, digestOK := typed["digest"].(string)
		revisionValue, revisionOK := typed["revision"].(float64)
		if idOK && digestOK && revisionOK && id == profileID && revisionValue == float64(revision) && len(digest) == 64 {
			count++
		}
		for _, child := range typed {
			count += countPortalProfileReferences(child, profileID, revision)
		}
		return count
	case []any:
		count := 0
		for _, child := range typed {
			count += countPortalProfileReferences(child, profileID, revision)
		}
		return count
	default:
		return 0
	}
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
