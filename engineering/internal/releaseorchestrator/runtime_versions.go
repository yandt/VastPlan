package releaseorchestrator

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

var pluginVersionIdentifiers = [][]byte{[]byte("PluginVersion"), []byte("pluginVersion")}

// SyncSelectedPluginRuntimeVersions projects the signed Manifest version into
// Go plugins that report their identity during the runtime handshake.
func SyncSelectedPluginRuntimeVersions(repositoryRoot string, workspace PluginWorkspace, selected map[string]string, write bool) ([]DerivedChange, error) {
	pluginIDs := make([]string, 0, len(selected))
	for pluginID := range selected {
		pluginIDs = append(pluginIDs, pluginID)
	}
	sort.Strings(pluginIDs)
	var changes []DerivedChange
	for _, pluginID := range pluginIDs {
		plugin, ok := workspace.Plugins[pluginID]
		if !ok {
			return nil, fmt.Errorf("runtime version 投影引用不存在的插件 %s", pluginID)
		}
		files, err := goSourceFiles(filepath.Join(repositoryRoot, filepath.FromSlash(plugin.Path)))
		if err != nil {
			return nil, err
		}
		for _, path := range files {
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			next, changed, err := replacePluginRuntimeVersion(raw, plugin.ID, plugin.Version)
			if err != nil {
				return nil, fmt.Errorf("解析 %s: %w", path, err)
			}
			if !changed {
				continue
			}
			relative, err := filepath.Rel(repositoryRoot, path)
			if err != nil {
				return nil, err
			}
			changes = append(changes, DerivedChange{Path: filepath.ToSlash(relative), Reason: "signed manifest runtime version projection"})
			if write {
				if err := os.WriteFile(path, next, 0o644); err != nil {
					return nil, err
				}
			}
		}
	}
	return changes, nil
}

func replacePluginRuntimeVersion(raw []byte, pluginID, version string) ([]byte, bool, error) {
	valueStart, valueEnd, found, err := pluginVersionBounds(raw)
	if err != nil {
		return raw, false, err
	}
	if !found {
		valueStart, valueEnd, found, err = identityTupleVersionBounds(raw, pluginID)
	}
	if err != nil || !found {
		return raw, false, err
	}
	if string(raw[valueStart:valueEnd]) == version {
		return raw, false, nil
	}
	next := make([]byte, 0, len(raw)+len(version))
	next = append(next, raw[:valueStart]...)
	next = append(next, version...)
	next = append(next, raw[valueEnd:]...)
	return next, true, nil
}

// identityTupleVersionBounds supports compact native plugins that declare their
// handshake identity as `const id, version, capability = "...", "...", "..."`.
func identityTupleVersionBounds(raw []byte, pluginID string) (int, int, bool, error) {
	const marker = "const id, version, capability"
	for offset := 0; offset < len(raw); {
		index := bytes.Index(raw[offset:], []byte(marker))
		if index < 0 {
			return 0, 0, false, nil
		}
		cursor := skipWhitespace(raw, offset+index+len(marker))
		if cursor >= len(raw) || raw[cursor] != '=' {
			offset += index + len(marker)
			continue
		}
		cursor = skipWhitespace(raw, cursor+1)
		idStart, idEnd, ok := quotedValueBounds(raw, cursor)
		if !ok || string(raw[idStart:idEnd]) != pluginID {
			offset += index + len(marker)
			continue
		}
		cursor = skipWhitespace(raw, idEnd+1)
		if cursor >= len(raw) || raw[cursor] != ',' {
			return 0, 0, false, errors.New("插件身份元组缺少版本分隔符")
		}
		versionStart, versionEnd, ok := quotedValueBounds(raw, skipWhitespace(raw, cursor+1))
		if !ok {
			return 0, 0, false, errors.New("插件身份元组版本无效")
		}
		return versionStart, versionEnd, true, nil
	}
	return 0, 0, false, nil
}

func pluginVersionBounds(raw []byte) (int, int, bool, error) {
	for _, identifier := range pluginVersionIdentifiers {
		for offset := 0; offset < len(raw); {
			start := bytes.Index(raw[offset:], identifier)
			if start < 0 {
				break
			}
			start += offset
			cursor := skipWhitespace(raw, start+len(identifier))
			if cursor >= len(raw) || raw[cursor] != '=' {
				offset = start + len(identifier)
				continue
			}
			cursor = skipWhitespace(raw, cursor+1)
			if cursor >= len(raw) || raw[cursor] != '"' {
				offset = start + len(identifier)
				continue
			}
			valueStart, valueEnd, ok := quotedValueBounds(raw, cursor)
			if !ok {
				return 0, 0, false, errors.New("PluginVersion 字符串未结束")
			}
			return valueStart, valueEnd, true, nil
		}
	}
	return 0, 0, false, nil
}

func quotedValueBounds(raw []byte, quoteOffset int) (int, int, bool) {
	if quoteOffset >= len(raw) || raw[quoteOffset] != '"' {
		return 0, 0, false
	}
	valueStart := quoteOffset + 1
	valueEnd := bytes.IndexByte(raw[valueStart:], '"')
	if valueEnd < 0 {
		return 0, 0, false
	}
	return valueStart, valueStart + valueEnd, true
}

func skipWhitespace(raw []byte, offset int) int {
	for offset < len(raw) && (raw[offset] == ' ' || raw[offset] == '\t') {
		offset++
	}
	return offset
}

func goSourceFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".go" {
			files = append(files, path)
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}
