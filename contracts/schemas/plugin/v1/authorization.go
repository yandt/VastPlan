package pluginv1

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AuthorizationContract is the signed, plugin-owned source for permissions
// and the operations they guard. Roles are intentionally absent: the platform
// policy service composes catalog permissions into separately versioned roles.
type AuthorizationContract struct {
	Namespace       string                  `json:"namespace"`
	Permissions     []PermissionDeclaration `json:"permissions"`
	OperationGuards []OperationGuard        `json:"operationGuards"`
}

type PermissionDeclaration struct {
	Code           string `json:"code"`
	Title          string `json:"title"`
	Description    string `json:"description,omitempty"`
	Scope          string `json:"scope"`
	ResourceType   string `json:"resourceType,omitempty"`
	Risk           string `json:"risk"`
	Assignable     bool   `json:"assignable"`
	OfflineAllowed bool   `json:"offlineAllowed"`
}

// OperationGuard requires every listed permission. Approval is descriptive
// policy metadata; the target domain service must still enforce object state
// and separation-of-duties with the authenticated subject.
type OperationGuard struct {
	ExtensionPoint string   `json:"extensionPoint"`
	Capability     string   `json:"capability"`
	Operation      string   `json:"operation"`
	Permissions    []string `json:"permissions"`
	Access         string   `json:"access"`
	Approval       string   `json:"approval"`
}

func validateAuthorization(manifest Manifest) error {
	contract := manifest.Authorization
	if contract == nil {
		return nil
	}
	if strings.HasPrefix(contract.Namespace, "platform.") && manifest.Publisher != "vastplan" {
		return fmt.Errorf("外部发布者不得声明保留权限命名空间 %q", contract.Namespace)
	}
	declared := make(map[string]PermissionDeclaration, len(contract.Permissions))
	for _, permission := range contract.Permissions {
		if permission.Code != contract.Namespace && !strings.HasPrefix(permission.Code, contract.Namespace+".") {
			return fmt.Errorf("权限 %q 不属于清单命名空间 %q", permission.Code, contract.Namespace)
		}
		if manifest.Publisher != "vastplan" && permission.Code != manifest.ID && !strings.HasPrefix(permission.Code, manifest.ID+".") {
			return fmt.Errorf("外部插件权限 %q 必须属于插件 ID 命名空间 %q", permission.Code, manifest.ID)
		}
		if _, duplicate := declared[permission.Code]; duplicate {
			return fmt.Errorf("权限声明重复: %s", permission.Code)
		}
		if permission.Scope == "resource" && permission.ResourceType == "" {
			return fmt.Errorf("资源权限 %s 缺少 resourceType", permission.Code)
		}
		if permission.Scope != "resource" && permission.ResourceType != "" {
			return fmt.Errorf("非资源权限 %s 不得声明 resourceType", permission.Code)
		}
		if permission.OfflineAllowed && (permission.Scope == "platform" || permission.Risk == "high" || permission.Risk == "critical") {
			return fmt.Errorf("平台级或高风险权限 %s 不得离线授权", permission.Code)
		}
		declared[permission.Code] = permission
	}
	operations, err := backendToolOperations(manifest)
	if err != nil {
		return err
	}
	used := make(map[string]struct{}, len(declared))
	seenGuards := make(map[string]struct{}, len(contract.OperationGuards))
	for _, guard := range contract.OperationGuards {
		key := guard.ExtensionPoint + "\x00" + guard.Capability + "\x00" + guard.Operation
		if _, duplicate := seenGuards[key]; duplicate {
			return fmt.Errorf("操作权限守卫重复: %s/%s#%s", guard.ExtensionPoint, guard.Capability, guard.Operation)
		}
		seenGuards[key] = struct{}{}
		if _, exists := operations[key]; !exists {
			return fmt.Errorf("操作权限守卫未绑定本插件已声明操作: %s/%s#%s", guard.ExtensionPoint, guard.Capability, guard.Operation)
		}
		if guard.Capability != contract.Namespace && !strings.HasPrefix(guard.Capability, contract.Namespace+".") && !strings.HasPrefix(contract.Namespace, guard.Capability+".") {
			return fmt.Errorf("守卫 capability %q 与权限命名空间 %q 无关", guard.Capability, contract.Namespace)
		}
		for _, code := range guard.Permissions {
			if _, exists := declared[code]; !exists {
				return fmt.Errorf("操作 %s/%s#%s 引用了未声明权限 %s", guard.ExtensionPoint, guard.Capability, guard.Operation, code)
			}
			used[code] = struct{}{}
		}
	}
	for code := range declared {
		if _, exists := used[code]; !exists {
			return fmt.Errorf("权限 %s 未绑定任何操作", code)
		}
	}
	return nil
}

func backendToolOperations(manifest Manifest) (map[string]struct{}, error) {
	var backend struct {
		Tools []struct {
			ID          string `json:"id"`
			Subcommands []struct {
				Name string `json:"name"`
			} `json:"subcommands"`
		} `json:"tools"`
	}
	if raw := manifest.Contributes["backend"]; len(raw) != 0 {
		if err := json.Unmarshal(raw, &backend); err != nil {
			return nil, fmt.Errorf("解析授权目标 backend contributions: %w", err)
		}
	}
	out := map[string]struct{}{}
	for _, tool := range backend.Tools {
		for _, operation := range tool.Subcommands {
			out["tool.package\x00"+tool.ID+"\x00"+operation.Name] = struct{}{}
		}
	}
	return out, nil
}
