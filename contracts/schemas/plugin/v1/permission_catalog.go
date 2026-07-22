package pluginv1

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const PermissionCatalogSchemaVersion = "v1"

// PermissionCatalogSource binds a parsed manifest to the immutable artifact
// digest that supplied it. Callers must obtain both through Artifact Trust.
type PermissionCatalogSource struct {
	Manifest       Manifest
	ArtifactSHA256 string
}

type PermissionCatalogEntry struct {
	PermissionDeclaration
	PluginID       string `json:"pluginId"`
	PluginVersion  string `json:"pluginVersion"`
	Publisher      string `json:"publisher"`
	ArtifactSHA256 string `json:"artifactSha256"`
}

type PermissionOperationEntry struct {
	OperationGuard
	PluginID       string `json:"pluginId"`
	PluginVersion  string `json:"pluginVersion"`
	ArtifactSHA256 string `json:"artifactSha256"`
}

// PermissionCatalog is deterministic and content-bound. It is an input to a
// signed policy snapshot, not itself an authorization decision or role grant.
type PermissionCatalog struct {
	SchemaVersion string                     `json:"schemaVersion"`
	Permissions   []PermissionCatalogEntry   `json:"permissions"`
	Operations    []PermissionOperationEntry `json:"operations"`
	Digest        string                     `json:"digest"`
}

// ParsePermissionCatalog strictly parses a trusted-host catalog projection and
// re-computes its content digest. The caller still owns the artifact-trust
// boundary that supplied the source manifests.
func ParsePermissionCatalog(raw []byte) (PermissionCatalog, error) {
	var catalog PermissionCatalog
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalog); err != nil {
		return PermissionCatalog{}, fmt.Errorf("解析权限目录: %w", err)
	}
	if catalog.SchemaVersion != PermissionCatalogSchemaVersion || len(catalog.Permissions) == 0 || len(catalog.Operations) == 0 {
		return PermissionCatalog{}, fmt.Errorf("权限目录版本或内容无效")
	}
	permissionCodes := map[string]struct{}{}
	for _, permission := range catalog.Permissions {
		if permission.Code == "" || permission.PluginID == "" || permission.PluginVersion == "" || permission.Publisher == "" || len(permission.ArtifactSHA256) != 64 {
			return PermissionCatalog{}, fmt.Errorf("权限目录条目无效: %s", permission.Code)
		}
		if _, duplicate := permissionCodes[permission.Code]; duplicate {
			return PermissionCatalog{}, fmt.Errorf("权限目录代码重复: %s", permission.Code)
		}
		permissionCodes[permission.Code] = struct{}{}
	}
	operations := map[string]struct{}{}
	for _, operation := range catalog.Operations {
		key := operation.ExtensionPoint + "\x00" + operation.Capability + "\x00" + operation.Operation
		if operation.ExtensionPoint == "" || operation.Capability == "" || operation.Operation == "" || len(operation.Permissions) == 0 {
			return PermissionCatalog{}, fmt.Errorf("权限目录操作无效: %s/%s#%s", operation.ExtensionPoint, operation.Capability, operation.Operation)
		}
		if _, duplicate := operations[key]; duplicate {
			return PermissionCatalog{}, fmt.Errorf("权限目录操作重复: %s/%s#%s", operation.ExtensionPoint, operation.Capability, operation.Operation)
		}
		operations[key] = struct{}{}
		for _, code := range operation.Permissions {
			if _, exists := permissionCodes[code]; !exists {
				return PermissionCatalog{}, fmt.Errorf("权限目录操作引用未知权限: %s", code)
			}
		}
	}
	recomputed, err := PermissionCatalogDigest(catalog)
	if err != nil {
		return PermissionCatalog{}, err
	}
	if catalog.Digest != recomputed {
		return PermissionCatalog{}, fmt.Errorf("权限目录 digest 不匹配")
	}
	return catalog, nil
}

func BuildPermissionCatalog(sources []PermissionCatalogSource) (PermissionCatalog, error) {
	catalog := PermissionCatalog{SchemaVersion: PermissionCatalogSchemaVersion}
	permissions := map[string]string{}
	operations := map[string]string{}
	for _, source := range sources {
		manifest := source.Manifest
		if manifest.Authorization == nil {
			continue
		}
		if len(source.ArtifactSHA256) != 64 || source.ArtifactSHA256 != strings.ToLower(source.ArtifactSHA256) {
			return PermissionCatalog{}, fmt.Errorf("插件 %s 权限目录制品摘要必须是小写 SHA-256", manifest.ID)
		}
		if _, err := hex.DecodeString(source.ArtifactSHA256); err != nil {
			return PermissionCatalog{}, fmt.Errorf("插件 %s 权限目录制品摘要无效", manifest.ID)
		}
		if err := validateAuthorization(manifest); err != nil {
			return PermissionCatalog{}, fmt.Errorf("插件 %s 授权声明无效: %w", manifest.ID, err)
		}
		for _, permission := range manifest.Authorization.Permissions {
			if owner, duplicate := permissions[permission.Code]; duplicate {
				return PermissionCatalog{}, fmt.Errorf("权限代码冲突 %s: %s 与 %s", permission.Code, owner, manifest.ID)
			}
			permissions[permission.Code] = manifest.ID
			catalog.Permissions = append(catalog.Permissions, PermissionCatalogEntry{PermissionDeclaration: permission, PluginID: manifest.ID, PluginVersion: manifest.Version, Publisher: manifest.Publisher, ArtifactSHA256: source.ArtifactSHA256})
		}
		for _, guard := range manifest.Authorization.OperationGuards {
			key := guard.ExtensionPoint + "\x00" + guard.Capability + "\x00" + guard.Operation
			if owner, duplicate := operations[key]; duplicate {
				return PermissionCatalog{}, fmt.Errorf("操作权限冲突 %s/%s#%s: %s 与 %s", guard.ExtensionPoint, guard.Capability, guard.Operation, owner, manifest.ID)
			}
			operations[key] = manifest.ID
			cloned := guard
			cloned.Permissions = append([]string(nil), guard.Permissions...)
			sort.Strings(cloned.Permissions)
			catalog.Operations = append(catalog.Operations, PermissionOperationEntry{OperationGuard: cloned, PluginID: manifest.ID, PluginVersion: manifest.Version, ArtifactSHA256: source.ArtifactSHA256})
		}
	}
	if len(catalog.Permissions) == 0 || len(catalog.Operations) == 0 {
		return PermissionCatalog{}, fmt.Errorf("权限目录不能是空目录")
	}
	sort.Slice(catalog.Permissions, func(i, j int) bool { return catalog.Permissions[i].Code < catalog.Permissions[j].Code })
	sort.Slice(catalog.Operations, func(i, j int) bool {
		left, right := catalog.Operations[i], catalog.Operations[j]
		if left.ExtensionPoint != right.ExtensionPoint {
			return left.ExtensionPoint < right.ExtensionPoint
		}
		if left.Capability != right.Capability {
			return left.Capability < right.Capability
		}
		return left.Operation < right.Operation
	})
	digest, err := PermissionCatalogDigest(catalog)
	if err != nil {
		return PermissionCatalog{}, err
	}
	catalog.Digest = digest
	return catalog, nil
}

// PermissionCatalogDigest excludes the mutable digest field and hashes the
// canonical, already sorted catalog projection.
func PermissionCatalogDigest(catalog PermissionCatalog) (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion string                     `json:"schemaVersion"`
		Permissions   []PermissionCatalogEntry   `json:"permissions"`
		Operations    []PermissionOperationEntry `json:"operations"`
	}{SchemaVersion: catalog.SchemaVersion, Permissions: catalog.Permissions, Operations: catalog.Operations})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}
