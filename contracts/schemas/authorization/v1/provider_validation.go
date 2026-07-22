package authorizationv1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func ValidateProviderProfile(profile ProviderProfile) error {
	raw, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	if err := validateSchema(profileSchemaURL+"#/$defs/providerProfile", raw); err != nil {
		return err
	}
	return validateProviderProfileSemantics(profile)
}

func ParseProviderProfile(raw []byte) (ProviderProfile, error) {
	if len(raw) > MaxProviderDescriptorBytes {
		return ProviderProfile{}, fmt.Errorf("Provider Profile 超过 %d bytes", MaxProviderDescriptorBytes)
	}
	if err := validateSchema(profileSchemaURL+"#/$defs/providerProfile", raw); err != nil {
		return ProviderProfile{}, err
	}
	var profile ProviderProfile
	if err := decodeStrict(raw, &profile); err != nil {
		return ProviderProfile{}, err
	}
	if err := validateProviderProfileSemantics(profile); err != nil {
		return ProviderProfile{}, err
	}
	return profile, nil
}

func validateProviderProfileSemantics(profile ProviderProfile) error {
	if err := validateProviderRef(profile.Store, ProtocolStore); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if err := validateProviderRef(profile.Engine, ProtocolEngine); err != nil {
		return fmt.Errorf("engine: %w", err)
	}
	if profile.Directory != nil {
		if err := validateProviderRef(*profile.Directory, ProtocolDirectory); err != nil {
			return fmt.Errorf("directory: %w", err)
		}
	}
	seenExchange := map[string]struct{}{}
	for _, provider := range profile.Exchange {
		if err := validateProviderRef(provider, ProtocolExchange); err != nil {
			return fmt.Errorf("exchange: %w", err)
		}
		key := provider.ProviderID + "@" + provider.Version
		if _, duplicate := seenExchange[key]; duplicate {
			return fmt.Errorf("exchange Provider 重复: %s", key)
		}
		seenExchange[key] = struct{}{}
	}
	return nil
}

func validateProviderRef(provider ProviderRef, expectedProtocol string) error {
	if provider.Protocol != expectedProtocol {
		return fmt.Errorf("期望协议 %s，实际 %s", expectedProtocol, provider.Protocol)
	}
	return nil
}

func ValidateProviderDescriptor(descriptor ProviderDescriptor) error {
	raw, err := json.Marshal(descriptor)
	if err != nil {
		return err
	}
	if err := validateSchema(profileSchemaURL+"#/$defs/providerDescriptor", raw); err != nil {
		return err
	}
	return validateProviderDescriptorSemantics(descriptor)
}

func ParseProviderDescriptor(raw []byte) (ProviderDescriptor, error) {
	if len(raw) > MaxProviderDescriptorBytes {
		return ProviderDescriptor{}, fmt.Errorf("Provider Descriptor 超过 %d bytes", MaxProviderDescriptorBytes)
	}
	if err := validateSchema(profileSchemaURL+"#/$defs/providerDescriptor", raw); err != nil {
		return ProviderDescriptor{}, err
	}
	var descriptor ProviderDescriptor
	if err := decodeStrict(raw, &descriptor); err != nil {
		return ProviderDescriptor{}, err
	}
	if err := validateProviderDescriptorSemantics(descriptor); err != nil {
		return ProviderDescriptor{}, err
	}
	return descriptor, nil
}

func validateProviderDescriptorSemantics(descriptor ProviderDescriptor) error {
	seenProtocols := map[string]struct{}{}
	for _, support := range descriptor.Protocols {
		if _, duplicate := seenProtocols[support.Protocol]; duplicate {
			return fmt.Errorf("Provider 协议重复: %s", support.Protocol)
		}
		seenProtocols[support.Protocol] = struct{}{}
		if err := validateProtocolSupport(support); err != nil {
			return err
		}
	}
	if len(descriptor.ConfigurationSchema) > 64<<10 {
		return errors.New("Provider configurationSchema 超过 64KiB")
	}
	var root map[string]any
	if err := json.Unmarshal(descriptor.ConfigurationSchema, &root); err != nil || root == nil || root["type"] != "object" {
		return errors.New("Provider configurationSchema 必须是 object JSON Schema")
	}
	if ref := externalSchemaRef(root); ref != "" {
		return fmt.Errorf("Provider configurationSchema 不得引用外部 Schema %q", ref)
	}
	if property := configurationSecretProperty(root); property != "" {
		return fmt.Errorf("Provider configurationSchema 不得声明疑似秘密字段 %q；请使用托管凭证", property)
	}
	configurationCompiler := jsonschema.NewCompiler()
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(descriptor.ConfigurationSchema))
	if err != nil {
		return fmt.Errorf("解析 Provider configurationSchema: %w", err)
	}
	resource := "https://schemas.cdsoft.com.cn/vastplan/authorization/provider/" + descriptor.ProviderID + "/" + descriptor.Version + ".schema.json"
	if err := configurationCompiler.AddResource(resource, document); err != nil {
		return fmt.Errorf("登记 Provider configurationSchema: %w", err)
	}
	if _, err := configurationCompiler.Compile(resource); err != nil {
		return fmt.Errorf("编译 Provider configurationSchema: %w", err)
	}
	return nil
}

func validateProtocolSupport(support ProtocolSupport) error {
	required := stringSet(ProtocolOperations(support.Protocol))
	if len(required) == 0 {
		return fmt.Errorf("未知 Provider 协议: %s", support.Protocol)
	}
	offered := stringSet(support.Operations)
	if len(offered) != len(support.Operations) {
		return fmt.Errorf("协议 %s 存在重复 operation", support.Protocol)
	}
	for operation := range offered {
		if _, exists := required[operation]; !exists {
			return fmt.Errorf("协议 %s 声明未知操作 %s", support.Protocol, operation)
		}
	}
	for operation := range required {
		if _, exists := offered[operation]; !exists {
			return fmt.Errorf("协议 %s 缺少必需操作 %s", support.Protocol, operation)
		}
	}
	return nil
}

func externalSchemaRef(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "$ref" {
				if ref, ok := child.(string); ok && !strings.HasPrefix(ref, "#") {
					return ref
				}
			}
			if ref := externalSchemaRef(child); ref != "" {
				return ref
			}
		}
	case []any:
		for _, child := range typed {
			if ref := externalSchemaRef(child); ref != "" {
				return ref
			}
		}
	}
	return ""
}

func configurationSecretProperty(value any) string {
	typed, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if properties, ok := typed["properties"].(map[string]any); ok {
		for name := range properties {
			lower := strings.ToLower(name)
			for _, marker := range []string{"password", "secret", "token", "privatekey", "private_key", "material"} {
				if strings.Contains(lower, marker) {
					return name
				}
			}
		}
	}
	for _, child := range typed {
		if found := configurationSecretProperty(child); found != "" {
			return found
		}
		if list, ok := child.([]any); ok {
			for _, item := range list {
				if found := configurationSecretProperty(item); found != "" {
					return found
				}
			}
		}
	}
	return ""
}
