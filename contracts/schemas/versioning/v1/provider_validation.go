package versioningv1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func ValidateProviderDescriptor(descriptor ProviderDescriptor) error {
	raw, err := json.Marshal(descriptor)
	if err != nil {
		return err
	}
	if err := validateDefinition("providerDescriptor", raw); err != nil {
		return err
	}
	version, err := semver.StrictNewVersion(descriptor.Version)
	if err != nil || version.Prerelease() != "" || version.Metadata() != "" {
		return errors.New("Version Provider version 必须是稳定严格 SemVer")
	}
	if !descriptor.Capabilities.DetachedVersions || !descriptor.Capabilities.NamedHeads || !descriptor.Capabilities.StableHistory || descriptor.MaxContentBytes > MaxContentBytes {
		return errors.New("Version Provider 缺少 v1 必需能力或内容上限无效")
	}
	if err := validateStorageSemantics(descriptor); err != nil {
		return err
	}
	return validateConfigurationSchema(descriptor)
}

func validateStorageSemantics(descriptor ProviderDescriptor) error {
	switch descriptor.Protocol {
	case StorageProtocolFile:
		if descriptor.Consistency != ConsistencySingleWriter || descriptor.Durability != DurabilityLocal || descriptor.ClusterSafe {
			return errors.New("File Provider v1 只能声明 local/single-writer，不能声明 clusterSafe")
		}
	case StorageProtocolGit:
		if descriptor.Consistency != ConsistencyRefCAS {
			return errors.New("Git Provider v1 必须提供 ref-cas")
		}
	case StorageProtocolRelational:
		if descriptor.Consistency != ConsistencyLinearizable || descriptor.Durability != DurabilityShared || !descriptor.ClusterSafe {
			return errors.New("Relational Provider v1 必须提供 shared/linearizable/clusterSafe")
		}
	default:
		return errors.New("Version Provider protocol 无效")
	}
	return nil
}

func validateConfigurationSchema(descriptor ProviderDescriptor) error {
	if len(descriptor.ConfigurationSchema) == 0 || len(descriptor.ConfigurationSchema) > 64<<10 {
		return errors.New("Version Provider configurationSchema 大小无效")
	}
	var configuration map[string]any
	if err := json.Unmarshal(descriptor.ConfigurationSchema, &configuration); err != nil || configuration["type"] != "object" {
		return errors.New("Version Provider configurationSchema 根必须是 object")
	}
	if ref := externalReference(configuration); ref != "" {
		return fmt.Errorf("Version Provider configurationSchema 不得引用外部资源 %q", ref)
	}
	compiler := jsonschema.NewCompiler()
	resource := "urn:vastplan:version-provider:" + descriptor.ID + ":" + descriptor.Version
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(descriptor.ConfigurationSchema))
	if err != nil {
		return err
	}
	if err := compiler.AddResource(resource, document); err != nil {
		return err
	}
	if _, err := compiler.Compile(resource); err != nil {
		return fmt.Errorf("编译 Version Provider configurationSchema: %w", err)
	}
	return nil
}

func externalReference(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "$ref" {
				if ref, ok := child.(string); ok && !strings.HasPrefix(ref, "#") {
					return ref
				}
			}
			if ref := externalReference(child); ref != "" {
				return ref
			}
		}
	case []any:
		for _, child := range typed {
			if ref := externalReference(child); ref != "" {
				return ref
			}
		}
	}
	return ""
}
