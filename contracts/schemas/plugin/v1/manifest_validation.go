package pluginv1

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func validateBackgroundService(manifest Manifest) error {
	if manifest.Runtime == nil || !manifest.Runtime.BackgroundService {
		return nil
	}
	if manifest.Runtime.InstancePolicy != "leader" ||
		(manifest.Runtime.StateModel != "leader-owned" && manifest.Runtime.StateModel != "external-shared") ||
		manifest.Runtime.Routing != "leader" {
		return errors.New("runtime.backgroundService 只允许 leader、leader routing，以及 leader-owned/external-shared 状态")
	}
	if manifest.Configuration == nil || manifest.Configuration.Scope != "service" || manifest.Configuration.ApplyMode != "restart" {
		return errors.New("runtime.backgroundService 要求 service-scoped restart 配置")
	}
	var schema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(manifest.Configuration.Schema, &schema); err != nil {
		return errors.New("runtime.backgroundService 配置 Schema 无效")
	}
	requiredTenant := false
	for _, field := range schema.Required {
		if field == "tenantId" {
			requiredTenant = true
			break
		}
	}
	var tenantSchema struct {
		Type string `json:"type"`
	}
	if raw := schema.Properties["tenantId"]; len(raw) == 0 || json.Unmarshal(raw, &tenantSchema) != nil || tenantSchema.Type != "string" || !requiredTenant {
		return errors.New("runtime.backgroundService 配置 Schema 必须要求字符串 tenantId")
	}
	if _, ok := manifest.Engines["backend"]; !ok || manifest.Entry["backend"] == "" {
		return errors.New("runtime.backgroundService 要求 backend 执行入口")
	}
	if manifest.Execution != nil && manifest.Execution.Backend != nil && manifest.Execution.Backend.DynamicGo != nil {
		return errors.New("runtime.backgroundService 不允许 dynamic-go 内嵌入口")
	}
	return nil
}

func validateAuthenticationProviderDependencies(manifest Manifest) error {
	raw := manifest.Contributes["backend"]
	if len(raw) == 0 {
		return nil
	}
	var backend struct {
		Providers []struct {
			ID                   string   `json:"id"`
			RequiredCapabilities []string `json:"requiredCapabilities"`
		} `json:"authenticationProviders"`
	}
	if err := json.Unmarshal(raw, &backend); err != nil {
		return fmt.Errorf("解析 authenticationProviders: %w", err)
	}
	declared := map[string]struct{}{}
	if manifest.Runtime != nil {
		for _, requirement := range manifest.Runtime.Requires {
			declared[requirement.Capability] = struct{}{}
		}
	}
	for _, provider := range backend.Providers {
		for _, capability := range provider.RequiredCapabilities {
			if _, exists := declared[capability]; !exists {
				return fmt.Errorf("authentication Provider %s 依赖 %s，但未在 runtime.requires 声明", provider.ID, capability)
			}
		}
	}
	return nil
}

func validateConfiguration(manifest Manifest) error {
	contract := manifest.Configuration
	if contract == nil {
		return nil
	}
	if contract.ApplyMode == "restart" && contract.Scope != "service" {
		return errors.New("configuration restart 只允许 service scope")
	}
	if contract.Controller != nil {
		if contract.Scope != "service" || contract.ApplyMode != "hot" || contract.Controller.Protocol != ConfigurationControllerProtocol {
			return errors.New("configuration.controller 只允许 service + hot + configuration.v1")
		}
	}
	if contract.ApplyMode == "hot" && (contract.Scope == "tenant" || contract.Scope == "user") {
		found := false
		if manifest.Runtime != nil {
			for _, requirement := range manifest.Runtime.Requires {
				if requirement.Capability == ConfigurationScopedResolverCapability && requirement.Scope == "remote" && requirement.Kind == "strong" && requirement.Ready == "readiness" && requirement.FailurePolicy == "fail" {
					found = true
					break
				}
			}
		}
		if !found {
			return errors.New("tenant/user hot configuration 必须强依赖 configuration.scoped resolver")
		}
	}
	if (contract.ResourceController == nil) != (len(contract.ResourceCollections) == 0) {
		return errors.New("configuration.resourceController 与 resourceCollections 必须同时声明")
	}
	if contract.ResourceController != nil {
		if contract.Scope != "service" || contract.ResourceController.Protocol != ConfigurationResourceControllerProtocol {
			return errors.New("configuration.resourceController 只允许 service + configuration.resource.v1")
		}
	}
	var schema map[string]any
	if err := json.Unmarshal(contract.Schema, &schema); err != nil || schema == nil {
		return errors.New("configuration.schema 必须是 JSON Schema 对象")
	}
	if schema["type"] != "object" {
		return errors.New("configuration.schema 根类型必须是 object")
	}
	if additional, exists := schema["additionalProperties"]; !exists || additional != false {
		return errors.New("configuration.schema 必须显式 additionalProperties=false")
	}
	if err := rejectRemoteSchemaRefs(schema); err != nil {
		return err
	}
	seenIDs := map[string]struct{}{}
	seenPurposes := map[string]struct{}{}
	for _, field := range contract.ManagedCredentials {
		if _, duplicate := seenIDs[field.ID]; duplicate {
			return fmt.Errorf("configuration.managedCredentials id 重复: %q", field.ID)
		}
		if _, duplicate := seenPurposes[field.Purpose]; duplicate {
			return fmt.Errorf("configuration.managedCredentials purpose 重复: %q", field.Purpose)
		}
		seenIDs[field.ID] = struct{}{}
		seenPurposes[field.Purpose] = struct{}{}
	}
	seenCollections := map[string]struct{}{}
	for _, collection := range contract.ResourceCollections {
		if _, duplicate := seenCollections[collection.ID]; duplicate {
			return fmt.Errorf("configuration.resourceCollections id 重复: %q", collection.ID)
		}
		seenCollections[collection.ID] = struct{}{}
		if collection.Kind != "profile" || collection.MaxItems == 0 || collection.MaxItems > 256 || collection.MinItems > collection.MaxItems {
			return fmt.Errorf("configuration.resourceCollections %q 数量或 kind 无效", collection.ID)
		}
		var resourceSchema map[string]any
		if err := json.Unmarshal(collection.Schema, &resourceSchema); err != nil || resourceSchema == nil || resourceSchema["type"] != "object" || resourceSchema["additionalProperties"] != false {
			return fmt.Errorf("configuration.resourceCollections %q schema 必须是闭合 object", collection.ID)
		}
		if err := rejectRemoteSchemaRefs(resourceSchema); err != nil {
			return fmt.Errorf("configuration.resourceCollections %q: %w", collection.ID, err)
		}
		resourceFieldIDs, resourcePurposes := map[string]struct{}{}, map[string]struct{}{}
		for _, field := range collection.ManagedCredentials {
			if _, duplicate := resourceFieldIDs[field.ID]; duplicate {
				return fmt.Errorf("configuration.resourceCollections %q 托管凭证 id 重复: %q", collection.ID, field.ID)
			}
			if _, duplicate := resourcePurposes[field.Purpose]; duplicate {
				return fmt.Errorf("configuration.resourceCollections %q 托管凭证 purpose 重复: %q", collection.ID, field.Purpose)
			}
			resourceFieldIDs[field.ID], resourcePurposes[field.Purpose] = struct{}{}, struct{}{}
		}
	}
	return nil
}

func validateCompositionFeatures(manifest Manifest) error {
	if manifest.Composition == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(manifest.Composition.Features))
	var baseProperties map[string]any
	if manifest.Configuration != nil {
		var base map[string]any
		if err := json.Unmarshal(manifest.Configuration.Schema, &base); err == nil {
			baseProperties, _ = base["properties"].(map[string]any)
		}
	}
	for _, feature := range manifest.Composition.Features {
		if _, duplicate := seen[feature.ID]; duplicate {
			return fmt.Errorf("composition.features id 重复: %q", feature.ID)
		}
		seen[feature.ID] = struct{}{}
		if _, self := feature.Dependencies[manifest.ID]; self {
			return fmt.Errorf("composition.features %q 不能依赖自身插件", feature.ID)
		}
		if err := validateRuntimeRequirements(feature.RuntimeRequires); err != nil {
			return fmt.Errorf("composition.features %q: %w", feature.ID, err)
		}
		if len(feature.ConfigurationSchema) == 0 {
			continue
		}
		if manifest.Configuration == nil {
			return fmt.Errorf("composition.features %q 声明 configurationSchema，但插件没有 configuration 契约", feature.ID)
		}
		var schema map[string]any
		if err := json.Unmarshal(feature.ConfigurationSchema, &schema); err != nil || schema == nil {
			return fmt.Errorf("composition.features %q configurationSchema 必须是 JSON Schema 对象", feature.ID)
		}
		if schema["type"] != "object" {
			return fmt.Errorf("composition.features %q configurationSchema 根类型必须是 object", feature.ID)
		}
		if additional, exists := schema["additionalProperties"]; !exists || additional != false {
			return fmt.Errorf("composition.features %q configurationSchema 必须显式 additionalProperties=false", feature.ID)
		}
		if err := rejectRemoteSchemaRefs(schema); err != nil {
			return fmt.Errorf("composition.features %q: %w", feature.ID, err)
		}
		properties, _ := schema["properties"].(map[string]any)
		for property := range properties {
			if _, declared := baseProperties[property]; !declared {
				return fmt.Errorf("composition.features %q configurationSchema 引用了基础配置未声明的字段 %q", feature.ID, property)
			}
		}
		required, _ := schema["required"].([]any)
		for _, value := range required {
			property, _ := value.(string)
			if _, declared := baseProperties[property]; !declared {
				return fmt.Errorf("composition.features %q configurationSchema 要求了基础配置未声明的字段 %q", feature.ID, property)
			}
		}
	}
	return nil
}

func rejectRemoteSchemaRefs(value any) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "$ref" {
				ref, _ := child.(string)
				if !strings.HasPrefix(ref, "#/") {
					return fmt.Errorf("configuration.schema 禁止远端或非本地 $ref: %q", ref)
				}
			}
			if err := rejectRemoteSchemaRefs(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := rejectRemoteSchemaRefs(child); err != nil {
				return err
			}
		}
	}
	return nil
}
