package pluginv1

import (
	"encoding/json"
	"fmt"

	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
)

// APIContributions 是签名插件清单中的声明式 API 能力。它们由控制面消费，
// 不会像 tool.package 一样直接注册到协议总线。
type APIContributions struct {
	Contracts         []apiv1.ContractContribution         `json:"apiContracts,omitempty"`
	DataPlaneServices []apiv1.DataPlaneServiceContribution `json:"dataPlaneServices,omitempty"`
}

type apiContributionBackend struct {
	Tools []apiContributionTool `json:"tools,omitempty"`
	APIContributions
}

type apiContributionTool struct {
	ID          string                   `json:"id"`
	ServiceRole string                   `json:"service_role"`
	Subcommands []apiContributionCommand `json:"subcommands,omitempty"`
}

type apiContributionCommand struct {
	Name string `json:"name"`
}

// ManifestAPIContributions 返回已通过清单 Schema 和所有权校验的 API 声明。
func ManifestAPIContributions(manifest Manifest) (APIContributions, error) {
	backend, err := decodeAPIContributionBackend(manifest)
	if err != nil {
		return APIContributions{}, err
	}
	return backend.APIContributions, nil
}

func validateAPIContributions(manifest Manifest) error {
	backend, err := decodeAPIContributionBackend(manifest)
	if err != nil {
		return err
	}
	tools := make(map[string]apiContributionTool, len(backend.Tools))
	for _, tool := range backend.Tools {
		tools[tool.ID] = tool
	}
	seenContracts := map[string]struct{}{}
	for _, contract := range backend.Contracts {
		if err := apiv1.ValidateContractContribution(contract); err != nil {
			return fmt.Errorf("apiContracts/%s: %w", contract.ID, err)
		}
		contractKey := contract.ContractID + "\x00" + contract.ContractVersion
		if _, duplicate := seenContracts[contractKey]; duplicate {
			return fmt.Errorf("apiContracts 契约及版本重复: %s@%s", contract.ContractID, contract.ContractVersion)
		}
		seenContracts[contractKey] = struct{}{}
		for _, route := range contract.Routes {
			tool, exists := tools[route.Target.Capability]
			if !exists {
				return fmt.Errorf("apiContracts/%s route %s 指向未由同一清单声明的 tool: %s", contract.ID, route.ID, route.Target.Capability)
			}
			if tool.ServiceRole != contract.ServiceRole {
				return fmt.Errorf("apiContracts/%s 与目标 tool %s 的 service_role 不一致", contract.ID, tool.ID)
			}
			if !toolDeclaresOperation(tool, route.Target.Operation) {
				return fmt.Errorf("apiContracts/%s route %s 指向未声明的 operation: %s/%s", contract.ID, route.ID, tool.ID, route.Target.Operation)
			}
		}
	}
	seenDataPlane := map[string]struct{}{}
	for _, service := range backend.DataPlaneServices {
		if err := apiv1.ValidateDataPlaneService(service); err != nil {
			return fmt.Errorf("dataPlaneServices/%s: %w", service.ID, err)
		}
		if _, duplicate := seenDataPlane[service.ID]; duplicate {
			return fmt.Errorf("dataPlaneServices id 重复: %s", service.ID)
		}
		seenDataPlane[service.ID] = struct{}{}
		if service.TicketTarget != nil {
			tool, exists := tools[service.TicketTarget.Capability]
			if !exists || tool.ServiceRole != service.ServiceRole || !toolDeclaresOperation(tool, service.TicketTarget.Operation) {
				return fmt.Errorf("dataPlaneServices/%s ticketTarget 未绑定同一清单拥有的 operation", service.ID)
			}
		}
	}
	return nil
}

func decodeAPIContributionBackend(manifest Manifest) (apiContributionBackend, error) {
	raw := manifest.Contributes["backend"]
	if len(raw) == 0 {
		return apiContributionBackend{}, nil
	}
	var backend apiContributionBackend
	if err := json.Unmarshal(raw, &backend); err != nil {
		return apiContributionBackend{}, fmt.Errorf("解析 API backend contributions: %w", err)
	}
	return backend, nil
}

func toolDeclaresOperation(tool apiContributionTool, operation string) bool {
	for _, command := range tool.Subcommands {
		if command.Name == operation {
			return true
		}
	}
	return false
}
