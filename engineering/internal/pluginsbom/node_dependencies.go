package pluginsbom

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cdsoft.com.cn/VastPlan/engineering/internal/cyclonedx"
)

type esbuildMetafile struct {
	Inputs  map[string]esbuildInput  `json:"inputs"`
	Outputs map[string]esbuildOutput `json:"outputs"`
}

type esbuildInput struct {
	Imports []esbuildImport `json:"imports"`
}

type esbuildOutput struct {
	Imports []esbuildImport `json:"imports"`
}

type esbuildImport struct {
	Path     string `json:"path"`
	External bool   `json:"external"`
}

type nodePackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Root    string `json:"-"`
}

func nodeDependencies(root, pluginDir string, metafiles []string) ([]cyclonedx.Component, error) {
	workspace, err := workspaceNodePackages(root)
	if err != nil {
		return nil, err
	}
	found := map[string]nodePackage{}
	for _, filename := range metafiles {
		var metadata esbuildMetafile
		if err := decodeJSONFile(filename, &metadata); err != nil {
			return nil, fmt.Errorf("读取 esbuild metafile %s: %w", filename, err)
		}
		if len(metadata.Inputs) == 0 || len(metadata.Outputs) == 0 {
			return nil, fmt.Errorf("esbuild metafile 缺少 inputs/outputs: %s", filename)
		}
		for input, facts := range metadata.Inputs {
			packageInfo, ok, err := packageForInput(root, pluginDir, input, workspace)
			if err != nil {
				return nil, err
			}
			if ok {
				found[packageInfo.Name] = packageInfo
			}
			for _, imported := range facts.Imports {
				if imported.External {
					if err := resolveExternalPackage(root, pluginDir, imported.Path, workspace, found); err != nil {
						return nil, err
					}
				}
			}
		}
		for _, output := range metadata.Outputs {
			for _, imported := range output.Imports {
				if imported.External {
					if err := resolveExternalPackage(root, pluginDir, imported.Path, workspace, found); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	sort.Strings(names)
	components := make([]cyclonedx.Component, 0, len(names))
	for _, name := range names {
		item := found[name]
		purl := npmPURL(item.Name, item.Version)
		components = append(components, cyclonedx.Component{Type: "library", BOMRef: purl, Name: item.Name, Version: item.Version, PURL: purl, Properties: []cyclonedx.Property{{Name: "vastplan:dependency.evidence", Value: "esbuild-metafile"}}})
	}
	return components, nil
}

func workspaceNodePackages(root string) (map[string]nodePackage, error) {
	packages := map[string]nodePackage{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".vastplan", "node_modules", "dist", "graphify-out":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if entry.Name() != "package.json" {
			return nil
		}
		item, err := readNodePackageLoose(filepath.Dir(path))
		if err != nil {
			return err
		}
		if item.Name == "" || item.Version == "" {
			return nil
		}
		if prior, exists := packages[item.Name]; exists && prior.Root != item.Root {
			return fmt.Errorf("Node workspace package 名称重复: %s", item.Name)
		}
		packages[item.Name] = item
		return nil
	})
	return packages, err
}

func packageForInput(root, pluginDir, input string, workspace map[string]nodePackage) (nodePackage, bool, error) {
	absolute := input
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(root, filepath.FromSlash(input))
	}
	absolute = filepath.Clean(absolute)
	if within(pluginDir, absolute) {
		return nodePackage{}, false, nil
	}
	var selected nodePackage
	for _, item := range workspace {
		if within(item.Root, absolute) && len(item.Root) > len(selected.Root) {
			selected = item
		}
	}
	if selected.Name != "" {
		return selected, true, nil
	}
	packageRoot := nodeModulesPackageRoot(absolute)
	if packageRoot == "" {
		return nodePackage{}, false, nil
	}
	item, err := readNodePackage(packageRoot)
	return item, err == nil, err
}

func resolveExternalPackage(root, pluginDir, specifier string, workspace map[string]nodePackage, found map[string]nodePackage) error {
	name := nodePackageName(specifier)
	if name == "" || nodeBuiltin(name) {
		return nil
	}
	if item, ok := workspace[name]; ok {
		if !within(pluginDir, item.Root) {
			found[name] = item
		}
		return nil
	}
	for _, base := range []string{filepath.Join(pluginDir, "backend", "node_modules"), filepath.Join(pluginDir, "frontend", "node_modules"), filepath.Join(root, "node_modules")} {
		item, err := readNodePackage(filepath.Join(base, filepath.FromSlash(name)))
		if err == nil {
			found[name] = item
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return fmt.Errorf("无法把 esbuild external %s 解析到已安装精确版本", specifier)
}

func readNodePackage(root string) (nodePackage, error) {
	item, err := readNodePackageLoose(root)
	if err != nil {
		return nodePackage{}, err
	}
	if item.Name == "" || item.Version == "" {
		return nodePackage{}, fmt.Errorf("Node package 缺少 name/version: %s", root)
	}
	return item, nil
}

func readNodePackageLoose(root string) (nodePackage, error) {
	raw, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return nodePackage{}, err
	}
	var item nodePackage
	if err := json.Unmarshal(raw, &item); err != nil {
		return nodePackage{}, err
	}
	item.Name, item.Version, item.Root = strings.TrimSpace(item.Name), strings.TrimSpace(item.Version), filepath.Clean(root)
	return item, nil
}

func nodeModulesPackageRoot(filename string) string {
	parts := strings.Split(filepath.ToSlash(filename), "/")
	index := -1
	for i, part := range parts {
		if part == "node_modules" {
			index = i
		}
	}
	if index < 0 || index+1 >= len(parts) {
		return ""
	}
	end := index + 2
	if strings.HasPrefix(parts[index+1], "@") {
		end++
	}
	if end > len(parts) {
		return ""
	}
	return filepath.FromSlash(strings.Join(parts[:end], "/"))
}

func nodePackageName(specifier string) string {
	specifier = strings.TrimSpace(specifier)
	if specifier == "" || strings.HasPrefix(specifier, "node:") || strings.HasPrefix(specifier, ".") || strings.HasPrefix(specifier, "/") {
		return ""
	}
	parts := strings.Split(specifier, "/")
	if strings.HasPrefix(specifier, "@") {
		if len(parts) < 2 {
			return ""
		}
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

func nodeBuiltin(name string) bool {
	switch name {
	case "assert", "async_hooks", "buffer", "child_process", "cluster", "console", "constants", "crypto", "dgram", "diagnostics_channel", "dns", "domain", "events", "fs", "http", "http2", "https", "inspector", "module", "net", "os", "path", "perf_hooks", "process", "punycode", "querystring", "readline", "repl", "sqlite", "stream", "string_decoder", "sys", "test", "timers", "tls", "trace_events", "tty", "url", "util", "v8", "vm", "wasi", "worker_threads", "zlib":
		return true
	default:
		return false
	}
}

func within(root, filename string) bool {
	relative, err := filepath.Rel(root, filename)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func npmPURL(name, version string) string {
	encoded := name
	if strings.HasPrefix(name, "@") {
		encoded = "%40" + strings.TrimPrefix(name, "@")
	}
	return "pkg:npm/" + encoded + "@" + version
}
