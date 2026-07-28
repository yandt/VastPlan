package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cdsoft.com.cn/VastPlan/engineering/internal/releaseorchestrator"
)

const (
	frontendHMRUIFamilyRender    = "render"
	frontendHMRUIFamilyShell     = "shell"
	frontendHMRUIFamilyWorkbench = "workbench"
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

// validateFrontendUIContractSources delegates to the release orchestrator so
// HMR, CI and release preparation enforce the same Contract Registry policy.
func validateFrontendUIContractSources(root string) error {
	changes, err := releaseorchestrator.SyncContracts(root, false)
	if err != nil {
		return err
	}
	if len(changes) != 0 {
		return fmt.Errorf("Contract Registry 派生文件未同步: %v", changes)
	}
	return nil
}
