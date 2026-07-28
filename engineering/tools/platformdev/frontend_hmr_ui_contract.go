package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	frontendHMRUIFamilyRender    = "render"
	frontendHMRUIFamilyShell     = "shell"
	frontendHMRUIFamilyWorkbench = "workbench"
)

var (
	frontendUIContractVersionPattern = regexp.MustCompile(`uiContractVersion\s*=\s*"([0-9]+\.[0-9]+\.[0-9]+)"`)
	frontendUIContractExportPattern  = regexp.MustCompile(`uiContract\s*:\s*"([0-9]+\.[0-9]+\.[0-9]+)"`)
)

type frontendHMRUIContract struct {
	Family string
	Range  string
}

type frontendUIManifest struct {
	Contributes struct {
		Frontend struct {
			RenderAdapters  []frontendUIContractValue `json:"renderAdapters"`
			RendererModules []frontendUIContractValue `json:"rendererModules"`
			Shells          []frontendUIContractValue `json:"shells"`
			ShellLibraries  []frontendUIContractValue `json:"shellLibraries"`
			Workbenches     []frontendUIContractValue `json:"workbenches"`
			Views           []frontendUIContractValue `json:"views"`
		} `json:"frontend"`
	} `json:"contributes"`
}

type frontendUIContractValue struct {
	UIContract string `json:"uiContract"`
}

func readFrontendHMRUIContract(root, pluginID string) (*frontendHMRUIContract, error) {
	if strings.TrimSpace(root) == "" {
		return nil, nil
	}
	pluginPath, err := developmentFrontendPluginPath(root, pluginID)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pluginPath), "vastplan.plugin.json"))
	if err != nil {
		return nil, err
	}
	var manifest frontendUIManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	frontend := manifest.Contributes.Frontend
	families := []struct {
		name   string
		values [][]frontendUIContractValue
	}{
		{name: frontendHMRUIFamilyRender, values: [][]frontendUIContractValue{frontend.RenderAdapters, frontend.RendererModules}},
		{name: frontendHMRUIFamilyShell, values: [][]frontendUIContractValue{frontend.Shells, frontend.ShellLibraries}},
		{name: frontendHMRUIFamilyWorkbench, values: [][]frontendUIContractValue{frontend.Workbenches}},
	}
	var selected *frontendHMRUIContract
	for _, family := range families {
		for _, values := range family.values {
			for _, value := range values {
				if value.UIContract == "" {
					return nil, fmt.Errorf("%s 贡献缺少 uiContract", family.name)
				}
				if selected == nil {
					selected = &frontendHMRUIContract{Family: family.name, Range: value.UIContract}
					continue
				}
				if selected.Family != family.name || selected.Range != value.UIContract {
					return nil, errors.New("同一插件声明了不一致的 UI 契约族或版本")
				}
			}
		}
	}
	return selected, nil
}

func overlayFrontendHMRUIContracts(document map[string]json.RawMessage, modules map[string]frontendHMRModule) error {
	portalRaw, ok := document["portal"]
	if !ok {
		return errors.New("RuntimeSpec 缺少 portal")
	}
	var portal map[string]any
	if err := json.Unmarshal(portalRaw, &portal); err != nil {
		return errors.New("RuntimeSpec portal 无效")
	}
	activeIDs, err := frontendHMRActiveModuleIDs(document)
	if err != nil {
		return err
	}
	ranges := map[string]string{}
	for id := range activeIDs {
		contract := modules[id].UIContract
		if contract == nil {
			continue
		}
		if existing, exists := ranges[contract.Family]; exists && existing != contract.Range {
			return fmt.Errorf("%s UI 契约未同步: %s 与 %s", contract.Family, existing, contract.Range)
		}
		ranges[contract.Family] = contract.Range
	}
	for family, field := range map[string]string{
		frontendHMRUIFamilyRender: "renderAdapter", frontendHMRUIFamilyShell: "shell", frontendHMRUIFamilyWorkbench: "workbench",
	} {
		contractRange, exists := ranges[family]
		if !exists {
			continue
		}
		selection, ok := portal[field].(map[string]any)
		if !ok {
			return fmt.Errorf("Portal 缺少 %s UI 契约选择", field)
		}
		selection["uiContract"] = contractRange
	}
	encoded, err := json.Marshal(portal)
	if err != nil {
		return err
	}
	document["portal"] = encoded
	return nil
}

func frontendHMRActiveModuleIDs(document map[string]json.RawMessage) (map[string]struct{}, error) {
	active := map[string]struct{}{}
	for _, key := range []string{"modules", "moduleGraphs"} {
		raw, exists := document[key]
		if !exists || string(raw) == "null" {
			continue
		}
		var descriptors []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &descriptors); err != nil {
			return nil, fmt.Errorf("RuntimeSpec %s 无效", key)
		}
		for _, descriptor := range descriptors {
			active[descriptor.ID] = struct{}{}
		}
	}
	return active, nil
}

// validateFrontendUIContractSources is the repository gate for a public UI
// contract change. Canonical SDK version, plugin declarations, Foundation
// module exports and the Seed Platform Catalog must move in one change.
func validateFrontendUIContractSources(root string) error {
	canonicalRaw, err := os.ReadFile(filepath.Join(root, "extensions", "sdk", "ts", "ui-contract", "src", "index.ts"))
	if err != nil {
		return err
	}
	match := frontendUIContractVersionPattern.FindSubmatch(canonicalRaw)
	if match == nil {
		return errors.New("无法读取 canonical UI Contract 版本")
	}
	version, contractRange := string(match[1]), "^"+string(match[1])
	var mismatches []string
	pluginRoot := filepath.Join(root, "extensions", "plugins")
	entries, err := os.ReadDir(pluginRoot)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pluginDir := filepath.Join(pluginRoot, entry.Name())
		manifestRaw, readErr := os.ReadFile(filepath.Join(pluginDir, "vastplan.plugin.json"))
		if readErr != nil {
			return readErr
		}
		var manifest any
		if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
			return fmt.Errorf("解析 %s 清单: %w", entry.Name(), err)
		}
		collectFrontendUIContractMismatches(manifest, contractRange, entry.Name()+" manifest", &mismatches)
		if !strings.HasPrefix(entry.Name(), "cn.vastplan.foundation.frontend.") {
			continue
		}
		sourceRoot := filepath.Join(pluginDir, "frontend", "src")
		if walkErr := filepath.WalkDir(sourceRoot, func(path string, item fs.DirEntry, walkErr error) error {
			if walkErr != nil || item.IsDir() || (filepath.Ext(path) != ".ts" && filepath.Ext(path) != ".tsx") {
				return walkErr
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for _, exported := range frontendUIContractExportPattern.FindAllSubmatch(raw, -1) {
				if string(exported[1]) != version {
					relative, _ := filepath.Rel(root, path)
					mismatches = append(mismatches, filepath.ToSlash(relative)+" exports "+string(exported[1]))
				}
			}
			return nil
		}); walkErr != nil {
			return walkErr
		}
	}
	catalogRaw, err := os.ReadFile(filepath.Join(root, "engineering", "deploy", "portal-platform-catalog.json"))
	if err != nil {
		return err
	}
	var catalog any
	if err := json.Unmarshal(catalogRaw, &catalog); err != nil {
		return err
	}
	collectFrontendUIContractMismatches(catalog, contractRange, "portal-platform-catalog.json", &mismatches)
	if len(mismatches) > 0 {
		sort.Strings(mismatches)
		return fmt.Errorf("UI Contract %s 兼容性同步不完整:\n- %s", version, strings.Join(mismatches, "\n- "))
	}
	return nil
}

func collectFrontendUIContractMismatches(value any, expected, location string, mismatches *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "uiContract" {
				if actual, ok := child.(string); ok && actual != expected {
					*mismatches = append(*mismatches, location+" declares "+actual)
				}
				continue
			}
			collectFrontendUIContractMismatches(child, expected, location, mismatches)
		}
	case []any:
		for _, child := range typed {
			collectFrontendUIContractMismatches(child, expected, location, mismatches)
		}
	}
}
