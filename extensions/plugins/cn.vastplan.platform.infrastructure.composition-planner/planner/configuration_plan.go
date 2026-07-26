package planner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/santhosh-tekuri/jsonschema/v6"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func configurationPlanForService(service backendcompositionv1.ServiceIntent, credentials map[string]map[string]commonv1.ManagedCredentialRef, application []string, paths map[string][]string, features map[string][]pluginv1.CompositionFeature, manifests map[string]pluginv1.Manifest) ([]backendcompositionv1.ConfigurationPlanItem, error) {
	roots := map[string]struct{}{}
	for _, root := range service.RootPlugins {
		roots[root.PluginID] = struct{}{}
	}
	var result []backendcompositionv1.ConfigurationPlanItem
	for _, id := range application {
		manifest := manifests[id]
		if manifest.Configuration == nil {
			if len(service.PluginConfig[id]) > 0 || len(credentials[id]) > 0 {
				return nil, fmt.Errorf("插件 %s 没有签名 configuration 契约，不能接受 pluginConfig", id)
			}
			continue
		}
		values := service.PluginConfig[id]
		if values == nil {
			values = map[string]any{}
		}
		schemaDigest, configurationDigest, missing, err := inspectConfiguration(manifest, values, credentials[id], features[id])
		if err != nil {
			return nil, fmt.Errorf("插件 %s 配置无效: %w", id, err)
		}
		source := "package-dependency"
		if _, root := roots[id]; root {
			source = "root"
		}
		result = append(result, backendcompositionv1.ConfigurationPlanItem{
			UnitID: service.ID, PluginID: id, Source: source, Editable: true,
			SchemaDigest: schemaDigest, ConfigurationDigest: configurationDigest,
			DependencyPath: append([]string(nil), paths[id]...), Missing: missing,
		})
	}
	return result, nil
}

func inspectConfiguration(manifest pluginv1.Manifest, values map[string]any, credentials map[string]commonv1.ManagedCredentialRef, features []pluginv1.CompositionFeature) (string, string, []backendcompositionv1.ConfigurationRequirement, error) {
	base, err := schemaObject(manifest.Configuration.Schema)
	if err != nil {
		return "", "", nil, err
	}
	schemaBinding := map[string]any{"base": base}
	var featureSchemas []any
	var missing []backendcompositionv1.ConfigurationRequirement
	missing = appendMissingProperties(missing, base, values)
	if err := validateWithoutRootRequired(base, values); err != nil {
		return "", "", nil, err
	}
	for _, feature := range features {
		if len(feature.ConfigurationSchema) == 0 {
			continue
		}
		schema, err := schemaObject(feature.ConfigurationSchema)
		if err != nil {
			return "", "", nil, err
		}
		featureSchemas = append(featureSchemas, schema)
		missing = appendMissingProperties(missing, schema, values)
		if err := validateWithoutRootRequired(schema, values); err != nil {
			return "", "", nil, fmt.Errorf("Feature %s: %w", feature.ID, err)
		}
	}
	if len(featureSchemas) > 0 {
		schemaBinding["features"] = featureSchemas
	}
	declared := map[string]pluginv1.ManagedCredentialField{}
	for _, field := range manifest.Configuration.ManagedCredentials {
		declared[field.ID] = field
		ref, configured := credentials[field.ID]
		if configured && (ref.Owner != manifest.ID || ref.Purpose != field.Purpose) {
			return "", "", nil, fmt.Errorf("托管凭证 %s 与签名 owner/purpose 不一致", field.ID)
		}
		if field.Required && !configured {
			missing = append(missing, backendcompositionv1.ConfigurationRequirement{Kind: "managed-credential", Field: field.ID})
		}
	}
	for field := range credentials {
		if _, exists := declared[field]; !exists {
			return "", "", nil, fmt.Errorf("Configuration Snapshot 包含未声明托管凭证字段 %s", field)
		}
	}
	missing = uniqueRequirements(missing)
	configuration := map[string]any{"values": values}
	if len(credentials) > 0 {
		configuration["managedCredentials"] = credentials
	}
	return compositioncommonv1.Digest(schemaBinding), compositioncommonv1.Digest(configuration), missing, nil
}

func schemaObject(raw json.RawMessage) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, fmt.Errorf("配置 Schema 必须是 JSON 对象")
	}
	return value, nil
}

func appendMissingProperties(target []backendcompositionv1.ConfigurationRequirement, schema, values map[string]any) []backendcompositionv1.ConfigurationRequirement {
	required, _ := schema["required"].([]any)
	for _, raw := range required {
		field, _ := raw.(string)
		if _, exists := values[field]; !exists {
			target = append(target, backendcompositionv1.ConfigurationRequirement{Kind: "property", Field: field})
		}
	}
	return target
}

func validateWithoutRootRequired(schema, values map[string]any) error {
	copy := cloneMap(schema)
	delete(copy, "required")
	raw, err := json.Marshal(copy)
	if err != nil {
		return fmt.Errorf("编码配置 Schema: %w", err)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("解析配置 Schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	const schemaURL = "urn:vastplan:planner:configuration"
	if err := compiler.AddResource(schemaURL, document); err != nil {
		return fmt.Errorf("登记配置 Schema: %w", err)
	}
	compiled, err := compiler.Compile(schemaURL)
	if err != nil {
		return fmt.Errorf("编译配置 Schema: %w", err)
	}
	if err := compiled.Validate(values); err != nil {
		return fmt.Errorf("配置值不符合签名 Schema: %w", err)
	}
	return nil
}

func cloneMap(source map[string]any) map[string]any {
	raw, _ := json.Marshal(source)
	var target map[string]any
	_ = json.Unmarshal(raw, &target)
	return target
}

func uniqueRequirements(values []backendcompositionv1.ConfigurationRequirement) []backendcompositionv1.ConfigurationRequirement {
	seen := map[string]struct{}{}
	result := values[:0]
	for _, value := range values {
		key := value.Kind + "\x00" + value.Field
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return result[i].Field < result[j].Field
	})
	return result
}
