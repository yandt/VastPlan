package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type frontendSourceSignatures struct{ plugins, host string }

type frontendPluginWatchState struct {
	shared  string
	plugins map[string]string
}

func (h *frontendHMR) pluginWatchState() (frontendPluginWatchState, error) {
	shared, err := sourceSignature(h.root, []string{"extensions/sdk/ts/platform-admin/src", "extensions/sdk/ts/platform-admin/package.json"})
	if err != nil {
		return frontendPluginWatchState{}, fmt.Errorf("扫描前端插件共享源码: %w", err)
	}
	plugins := map[string]string{}
	for _, relativeRoot := range []string{"extensions/plugins", "examples/plugins"} {
		directory := filepath.Join(h.root, filepath.FromSlash(relativeRoot))
		entries, err := os.ReadDir(directory)
		if err != nil {
			return frontendPluginWatchState{}, fmt.Errorf("列出前端插件 %s: %w", relativeRoot, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			manifestPath := filepath.Join(directory, entry.Name(), "vastplan.plugin.json")
			raw, err := os.ReadFile(manifestPath)
			if err != nil {
				return frontendPluginWatchState{}, fmt.Errorf("读取插件清单 %s: %w", entry.Name(), err)
			}
			var manifest struct {
				Entry struct {
					Frontend string `json:"frontend"`
				} `json:"entry"`
			}
			if err := json.Unmarshal(raw, &manifest); err != nil {
				return frontendPluginWatchState{}, fmt.Errorf("解析插件清单 %s: %w", entry.Name(), err)
			}
			if strings.TrimSpace(manifest.Entry.Frontend) == "" {
				continue
			}
			if _, duplicate := plugins[entry.Name()]; duplicate {
				return frontendPluginWatchState{}, fmt.Errorf("产品与示例前端插件 ID 重复: %s", entry.Name())
			}
			relative := filepath.ToSlash(filepath.Join(relativeRoot, entry.Name()))
			signature, err := sourceSignature(h.root, []string{relative})
			if err != nil {
				return frontendPluginWatchState{}, fmt.Errorf("扫描前端插件 %s: %w", entry.Name(), err)
			}
			plugins[entry.Name()] = signature
		}
	}
	return frontendPluginWatchState{shared: shared, plugins: plugins}, nil
}

func changedFrontendPlugins(previous, next frontendPluginWatchState) ([]string, bool) {
	if previous.shared != next.shared || len(previous.plugins) != len(next.plugins) {
		return nil, true
	}
	changed := make([]string, 0)
	for id, signature := range next.plugins {
		previousSignature, exists := previous.plugins[id]
		if !exists {
			return nil, true
		}
		if previousSignature != signature {
			changed = append(changed, id)
		}
	}
	sort.Strings(changed)
	return changed, false
}

func (h *frontendHMR) sourceSignatures() (frontendSourceSignatures, error) {
	return h.sourceSignaturesFor(nil)
}

func (h *frontendHMR) sourceSignaturesFor(pluginIDs []string) (frontendSourceSignatures, error) {
	pluginPaths := []string{"extensions/sdk/ts/platform-admin/src", "extensions/sdk/ts/platform-admin/package.json"}
	if pluginIDs == nil {
		pluginPaths = append(pluginPaths, "extensions/plugins", "examples/plugins")
	} else {
		for _, id := range pluginIDs {
			path, err := developmentFrontendPluginPath(h.root, id)
			if err != nil {
				return frontendSourceSignatures{}, err
			}
			pluginPaths = append(pluginPaths, path)
		}
	}
	plugins, err := sourceSignature(h.root, pluginPaths)
	if err != nil {
		return frontendSourceSignatures{}, fmt.Errorf("扫描前端插件源码: %w", err)
	}
	host, err := sourceSignature(h.root, []string{
		"core/kernels/frontend/src", "core/kernels/frontend/static", "core/kernels/frontend/package.json",
		"extensions/sdk/ts/icon-catalog/src", "extensions/sdk/ts/icon-catalog/package.json",
		"extensions/sdk/ts/ui-primitives/src", "extensions/sdk/ts/ui-primitives/package.json",
		"extensions/sdk/ts/rjsf-csp-validator/src", "extensions/sdk/ts/rjsf-csp-validator/package.json",
		"extensions/sdk/ts/ui-contract/src", "extensions/sdk/ts/ui-contract/package.json",
		"extensions/sdk/ts/workbench-sdk/src", "extensions/sdk/ts/workbench-sdk/package.json",
		"engineering/tools/build-frontend.sh", "engineering/tools/build-frontend-plugins.mjs", "engineering/tools/check-ant-icon-catalog-on-demand.mjs",
		"engineering/tools/frontend-module-graph.mjs", "engineering/tools/frontend-server-build.mjs",
		"package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "tsconfig.base.json",
	})
	if err != nil {
		return frontendSourceSignatures{}, fmt.Errorf("扫描 Portal 宿主源码: %w", err)
	}
	return frontendSourceSignatures{plugins: plugins, host: host}, nil
}

func developmentFrontendPluginPath(root, id string) (string, error) {
	for _, relativeRoot := range []string{"extensions/plugins", "examples/plugins"} {
		relative := filepath.ToSlash(filepath.Join(relativeRoot, id))
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); err == nil && info.IsDir() {
			return relative, nil
		}
	}
	return "", fmt.Errorf("找不到前端插件源码目录: %s", id)
}

func sourceSignature(root string, relativePaths []string) (string, error) {
	hash := sha256.New()
	for _, relativeRoot := range relativePaths {
		absoluteRoot := filepath.Join(root, filepath.FromSlash(relativeRoot))
		err := filepath.WalkDir(absoluteRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "node_modules" || entry.Name() == "dist" {
					return filepath.SkipDir
				}
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			switch filepath.Ext(path) {
			case ".ts", ".tsx", ".css", ".json", ".mjs", ".sh", ".html":
			default:
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			_, _ = io.WriteString(hash, filepath.ToSlash(relative))
			_, _ = hash.Write([]byte{0})
			_, _ = hash.Write(content)
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
