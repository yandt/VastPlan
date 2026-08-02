package frontendcompositionv1

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/santhosh-tekuri/jsonschema/v6"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/configfile"
)

func ParseApplicationComposition(raw []byte) (ApplicationComposition, error) {
	_, a, _, err := schemas()
	if err != nil {
		return ApplicationComposition{}, err
	}
	if err := validateJSON(a, raw, "Frontend Application Composition"); err != nil {
		return ApplicationComposition{}, err
	}
	var value ApplicationComposition
	if err := json.Unmarshal(raw, &value); err != nil {
		return ApplicationComposition{}, err
	}
	if err := compositioncommonv1.ValidateTarget(value.Target, compositioncommonv1.KernelFrontend); err != nil {
		return ApplicationComposition{}, err
	}
	value.Plugins, err = normalizeRefs(value.Plugins)
	if err != nil {
		return ApplicationComposition{}, err
	}
	return value, nil
}

func ParsePortalPlatformCatalog(raw []byte) (PortalPlatformCatalog, error) {
	_, _, schema, err := schemas()
	if err != nil {
		return PortalPlatformCatalog{}, err
	}
	if err := validateJSON(schema, raw, "Portal Platform Catalog"); err != nil {
		return PortalPlatformCatalog{}, err
	}
	var value PortalPlatformCatalog
	if err := json.Unmarshal(raw, &value); err != nil {
		return PortalPlatformCatalog{}, err
	}
	return validatePortalPlatformCatalog(value)
}

func ValidatePlatformProfile(v PlatformProfile) (PlatformProfile, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return PlatformProfile{}, err
	}
	return ParsePlatformProfile(raw)
}
func ValidateApplicationComposition(v ApplicationComposition) (ApplicationComposition, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return ApplicationComposition{}, err
	}
	return ParseApplicationComposition(raw)
}
func ValidatePortalPlatformCatalog(v PortalPlatformCatalog) (PortalPlatformCatalog, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return PortalPlatformCatalog{}, err
	}
	return ParsePortalPlatformCatalog(raw)
}
func ParsePlatformProfileFile(path string) (PlatformProfile, error) {
	raw, err := configfile.Load(path)
	if err != nil {
		return PlatformProfile{}, err
	}
	return ParsePlatformProfile(raw)
}
func ParseApplicationCompositionFile(path string) (ApplicationComposition, error) {
	raw, err := configfile.Load(path)
	if err != nil {
		return ApplicationComposition{}, err
	}
	return ParseApplicationComposition(raw)
}
func ParsePortalPlatformCatalogFile(path string) (PortalPlatformCatalog, error) {
	raw, err := configfile.Load(path)
	if err != nil {
		return PortalPlatformCatalog{}, err
	}
	return ParsePortalPlatformCatalog(raw)
}
func (v PlatformProfile) Digest() string        { return compositioncommonv1.Digest(v) }
func (v ApplicationComposition) Digest() string { return compositioncommonv1.Digest(v) }
func (v PortalPlatformCatalog) Digest() string  { return compositioncommonv1.Digest(v) }

func (v PortalPlatformCatalog) Resolve(tenantID, portalID string) (PlatformProfile, PortalBinding, error) {
	for _, binding := range v.Bindings {
		if binding.TenantID != tenantID || binding.PortalID != portalID {
			continue
		}
		for _, profile := range v.Profiles {
			if profile.ID == binding.PlatformProfile.ID {
				return profile, binding, nil
			}
		}
	}
	return PlatformProfile{}, PortalBinding{}, fmt.Errorf("Portal %s/%s 没有平台管理绑定", tenantID, portalID)
}

func validatePortalPlatformCatalog(value PortalPlatformCatalog) (PortalPlatformCatalog, error) {
	profiles := make(map[string]PlatformProfile, len(value.Profiles))
	for i := range value.Profiles {
		profile, err := ValidatePlatformProfile(value.Profiles[i])
		if err != nil {
			return PortalPlatformCatalog{}, fmt.Errorf("验证 Platform Profile %q: %w", value.Profiles[i].ID, err)
		}
		if _, duplicate := profiles[profile.ID]; duplicate {
			return PortalPlatformCatalog{}, fmt.Errorf("Platform Profile id 重复: %s", profile.ID)
		}
		profiles[profile.ID] = profile
		value.Profiles[i] = profile
	}
	seenBindings := map[string]struct{}{}
	for i := range value.Bindings {
		binding := &value.Bindings[i]
		bindingKey := binding.TenantID + "\x00" + binding.PortalID
		if _, duplicate := seenBindings[bindingKey]; duplicate {
			return PortalPlatformCatalog{}, fmt.Errorf("Portal 平台绑定重复: %s/%s", binding.TenantID, binding.PortalID)
		}
		seenBindings[bindingKey] = struct{}{}
		if err := ValidatePortalBinding(*binding); err != nil {
			return PortalPlatformCatalog{}, fmt.Errorf("Portal %s/%s 管理绑定无效: %w", binding.TenantID, binding.PortalID, err)
		}
		profile, ok := profiles[binding.PlatformProfile.ID]
		if !ok || binding.PlatformProfile.Revision != profile.Revision || binding.PlatformProfile.Digest != profile.Digest() {
			return PortalPlatformCatalog{}, fmt.Errorf("Portal %s/%s 的 Platform Profile 锁无效", binding.TenantID, binding.PortalID)
		}
	}
	return value, nil
}

func ValidatePortalBinding(binding PortalBinding) error {
	if binding.TenantID == "" || !managementName.MatchString(binding.PortalID) {
		return fmt.Errorf("tenantId 或 portalId 无效")
	}
	if binding.PlatformProfile.ID == "" || binding.PlatformProfile.Revision == 0 || len(binding.PlatformProfile.Digest) != 64 {
		return fmt.Errorf("Platform Profile 引用无效")
	}
	if _, err := hex.DecodeString(binding.PlatformProfile.Digest); err != nil {
		return fmt.Errorf("Platform Profile 摘要无效")
	}
	if len(binding.Services) == 0 {
		return fmt.Errorf("Portal 至少需要一个受管服务")
	}
	return validateManagedServices(binding.Services)
}

var managementName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$`)
var templateName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var managementContractID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$`)
var managementContractVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

func validateManagedServices(services []ManagedService) error {
	seenIDs, seenTargets := map[string]struct{}{}, map[string]struct{}{}
	for _, service := range services {
		if !managementName.MatchString(service.ID) || !managementName.MatchString(service.LogicalService) || !managementName.MatchString(service.RoutingDomain) {
			return fmt.Errorf("服务 id、logicalService 或 routingDomain 格式无效: %s", service.ID)
		}
		if _, duplicate := seenIDs[service.ID]; duplicate {
			return fmt.Errorf("服务 id 重复: %s", service.ID)
		}
		seenIDs[service.ID] = struct{}{}
		if service.Resource != nil && (service.Resource.Kind != "service-unit" || service.Resource.Kernel != "backend" || !managementName.MatchString(service.Resource.Deployment) || !managementName.MatchString(service.Resource.UnitID)) {
			return fmt.Errorf("服务受管资源无效: %s", service.ID)
		}
		target := service.LogicalService + "\x00" + service.RoutingDomain
		if _, duplicate := seenTargets[target]; duplicate {
			return fmt.Errorf("服务路由目标重复: %s/%s", service.LogicalService, service.RoutingDomain)
		}
		seenTargets[target] = struct{}{}
		seenCapabilities := map[string]struct{}{}
		for _, grant := range service.Capabilities {
			if !managementName.MatchString(grant.Capability) {
				return fmt.Errorf("capability 格式无效: %s", grant.Capability)
			}
			if _, duplicate := seenCapabilities[grant.Capability]; duplicate {
				return fmt.Errorf("capability 重复: %s", grant.Capability)
			}
			seenCapabilities[grant.Capability] = struct{}{}
			seenOperations := map[string]struct{}{}
			for _, operation := range append(append([]string(nil), grant.Read...), grant.Write...) {
				if !managementName.MatchString(operation) {
					return fmt.Errorf("operation 格式无效: %s", operation)
				}
				if _, duplicate := seenOperations[operation]; duplicate {
					return fmt.Errorf("operation 在 read/write 中重复: %s/%s", grant.Capability, operation)
				}
				seenOperations[operation] = struct{}{}
			}
			if len(seenOperations) == 0 {
				return fmt.Errorf("capability 未授予任何 operation: %s", grant.Capability)
			}
		}
		if len(seenCapabilities) == 0 {
			return fmt.Errorf("服务未授予任何 capability: %s", service.ID)
		}
		seenAPIs, seenContracts := map[string]struct{}{}, map[string]struct{}{}
		if len(service.APIs) > 32 {
			return fmt.Errorf("服务 %s 的 Management API 数量超过上限", service.ID)
		}
		for _, api := range service.APIs {
			if !managementName.MatchString(api.ID) || !managementContractID.MatchString(api.ContractID) || !managementContractVersion.MatchString(api.ContractVersion) {
				return fmt.Errorf("服务 %s 的 Management API 引用格式无效: %s", service.ID, api.ID)
			}
			if len(api.ContractDigest) != 64 {
				return fmt.Errorf("服务 %s 的 Management API 摘要无效: %s", service.ID, api.ID)
			}
			if _, err := hex.DecodeString(api.ContractDigest); err != nil {
				return fmt.Errorf("服务 %s 的 Management API 摘要无效: %s", service.ID, api.ID)
			}
			if _, duplicate := seenAPIs[api.ID]; duplicate {
				return fmt.Errorf("服务 %s 的 Management API id 重复: %s", service.ID, api.ID)
			}
			seenAPIs[api.ID] = struct{}{}
			contract := api.ContractID + "\x00" + api.ContractVersion + "\x00" + api.ContractDigest
			if _, duplicate := seenContracts[contract]; duplicate {
				return fmt.Errorf("服务 %s 的 Management API 契约重复: %s", service.ID, api.ContractID)
			}
			seenContracts[contract] = struct{}{}
		}
	}
	return nil
}

func validateJSON(schema *jsonschema.Schema, raw []byte, noun string) error {
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("解析 %s JSON: %w", noun, err)
	}
	if err := schema.Validate(instance); err != nil {
		return fmt.Errorf("%s 不符合 Schema: %w", noun, err)
	}
	return nil
}
func normalizeRefs(refs []PluginRef) ([]PluginRef, error) {
	out := make([]PluginRef, len(refs))
	copy(out, refs)
	seen := map[string]struct{}{}
	for i := range out {
		out[i].Channel = channel(out[i].Channel)
		if _, ok := seen[out[i].ID]; ok {
			return nil, fmt.Errorf("Frontend 组合插件 id 重复: %q", out[i].ID)
		}
		seen[out[i].ID] = struct{}{}
	}
	return out, nil
}
func channel(v string) string {
	if v == "" {
		return "stable"
	}
	return v
}
func same(a, b PluginRef) bool {
	return a.ID == b.ID && a.Version == b.Version && channel(a.Channel) == channel(b.Channel)
}
