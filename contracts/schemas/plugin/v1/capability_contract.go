package pluginv1

import (
	"encoding/json"
	"fmt"
	"sort"
)

// ToolCapabilityContract is the normalized, signed source for one tool
// capability. Runtime descriptors, permission catalogs and trusted host
// projections must derive from this view instead of maintaining operation
// lists independently.
type ToolCapabilityContract struct {
	PluginID      string                  `json:"pluginId"`
	PluginVersion string                  `json:"pluginVersion"`
	Capability    string                  `json:"capability"`
	ServiceRole   string                  `json:"serviceRole"`
	Title         string                  `json:"title,omitempty"`
	Operations    []ToolOperationContract `json:"operations"`
}

type ToolOperationContract struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Audience     string          `json:"audience,omitempty"`
	ParamsSchema json.RawMessage `json:"paramsSchema,omitempty"`
	ResultSchema json.RawMessage `json:"resultSchema,omitempty"`
	Guard        *OperationGuard `json:"guard,omitempty"`
}

type manifestTool struct {
	ID          string                   `json:"id"`
	ServiceRole string                   `json:"service_role"`
	Title       string                   `json:"title,omitempty"`
	Subcommands []manifestToolSubcommand `json:"subcommands,omitempty"`
}

type manifestToolSubcommand struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Audience     string          `json:"audience,omitempty"`
	ParamsSchema json.RawMessage `json:"paramsSchema,omitempty"`
	ResultSchema json.RawMessage `json:"resultSchema,omitempty"`
}

// ManifestToolCapabilityContracts normalizes backend tool declarations and
// joins their authorization guards. When one tool opts into the audience
// marker, every operation in that tool must be classified and user operations
// must have exactly one signed guard. This enables gradual migration without
// weakening legacy plugins.
func ManifestToolCapabilityContracts(manifest Manifest) ([]ToolCapabilityContract, error) {
	var backend struct {
		Tools []manifestTool `json:"tools"`
	}
	if raw := manifest.Contributes["backend"]; len(raw) != 0 {
		if err := json.Unmarshal(raw, &backend); err != nil {
			return nil, fmt.Errorf("解析 capability contracts: %w", err)
		}
	}
	guards := map[string]OperationGuard{}
	if manifest.Authorization != nil {
		for _, guard := range manifest.Authorization.OperationGuards {
			key := guard.ExtensionPoint + "\x00" + guard.Capability + "\x00" + guard.Operation
			guards[key] = guard
		}
	}
	seenTools := map[string]struct{}{}
	contracts := make([]ToolCapabilityContract, 0, len(backend.Tools))
	for _, tool := range backend.Tools {
		if _, duplicate := seenTools[tool.ID]; duplicate {
			return nil, fmt.Errorf("tool capability 重复: %s", tool.ID)
		}
		seenTools[tool.ID] = struct{}{}
		seenOperations := map[string]struct{}{}
		classified := false
		operations := make([]ToolOperationContract, 0, len(tool.Subcommands))
		for _, operation := range tool.Subcommands {
			if _, duplicate := seenOperations[operation.Name]; duplicate {
				return nil, fmt.Errorf("tool %s operation 重复: %s", tool.ID, operation.Name)
			}
			seenOperations[operation.Name] = struct{}{}
			classified = classified || operation.Audience != ""
			guard, guarded := guards["tool.package\x00"+tool.ID+"\x00"+operation.Name]
			var guardRef *OperationGuard
			if guarded {
				copy := guard
				copy.Permissions = append([]string(nil), guard.Permissions...)
				guardRef = &copy
			}
			operations = append(operations, ToolOperationContract{
				Name: operation.Name, Description: operation.Description, Audience: operation.Audience,
				ParamsSchema: append(json.RawMessage(nil), operation.ParamsSchema...), ResultSchema: append(json.RawMessage(nil), operation.ResultSchema...), Guard: guardRef,
			})
		}
		if classified {
			for _, operation := range operations {
				if operation.Audience == "" {
					return nil, fmt.Errorf("tool %s 已启用统一 Capability Contract，但 operation %s 缺少 audience", tool.ID, operation.Name)
				}
				if operation.Audience == "user" && operation.Guard == nil {
					return nil, fmt.Errorf("tool %s 的用户 operation %s 缺少 authorization guard", tool.ID, operation.Name)
				}
				if operation.Audience != "user" && operation.Guard != nil {
					return nil, fmt.Errorf("tool %s 的 %s operation %s 不得声明用户 authorization guard", tool.ID, operation.Audience, operation.Name)
				}
			}
		}
		contracts = append(contracts, ToolCapabilityContract{
			PluginID: manifest.ID, PluginVersion: manifest.Version, Capability: tool.ID,
			ServiceRole: tool.ServiceRole, Title: tool.Title, Operations: operations,
		})
	}
	sort.Slice(contracts, func(left, right int) bool { return contracts[left].Capability < contracts[right].Capability })
	return contracts, nil
}
