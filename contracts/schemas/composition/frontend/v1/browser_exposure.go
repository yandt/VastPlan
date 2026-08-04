package frontendcompositionv1

import (
	"fmt"
	"sort"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

// ManifestResolver returns the already verified manifest selected by one
// Platform Profile reference. The composition package never reads artifacts or
// repository credentials itself; production composition roots provide this
// function after verification, while development passes local signed sources.
type ManifestResolver func(PluginRef) (pluginv1.Manifest, error)

// CompilePortalBrowserExposure materializes the runtime-only operation grants
// from plugin-owned manifest declarations and platform-owned disable policy.
// A source catalog selects capabilities, never individual operations.
func CompilePortalBrowserExposure(catalog PortalPlatformCatalog, resolveManifest ManifestResolver) (PortalPlatformCatalog, error) {
	if resolveManifest == nil {
		return PortalPlatformCatalog{}, fmt.Errorf("浏览器暴露编译缺少已验证 Manifest 解析器")
	}
	value, err := ValidatePortalPlatformCatalog(catalog)
	if err != nil {
		return PortalPlatformCatalog{}, err
	}
	compiled := clonePortalPlatformCatalog(value)
	profiles := make(map[string]PlatformProfile, len(compiled.Profiles))
	for _, profile := range compiled.Profiles {
		profiles[profile.ID] = profile
	}
	policy := newBrowserExposurePolicy(compiled.BrowserExposure)
	seenDisabledOperations := map[string]bool{}
	seenDisabledPlugins := map[string]bool{}

	for bindingIndex := range compiled.Bindings {
		binding := &compiled.Bindings[bindingIndex]
		profile := profiles[binding.PlatformProfile.ID]
		manifests, err := profileBrowserExposureManifests(profile, resolveManifest)
		if err != nil {
			return PortalPlatformCatalog{}, fmt.Errorf("编译 Portal %s/%s 浏览器暴露: %w", binding.TenantID, binding.PortalID, err)
		}
		for serviceIndex := range binding.Services {
			service := &binding.Services[serviceIndex]
			for capabilityIndex := range service.Capabilities {
				grant := &service.Capabilities[capabilityIndex]
				if len(grant.Read) != 0 || len(grant.Write) != 0 {
					return PortalPlatformCatalog{}, fmt.Errorf("Portal 服务 %s 的 capability %s 不得在 Catalog 中手工声明 read/write", service.ID, grant.Capability)
				}
				operations, usedDisables, usedPlugins, err := compileCapabilityExposure(manifests, policy, grant.Capability)
				if err != nil {
					return PortalPlatformCatalog{}, fmt.Errorf("Portal 服务 %s 的 capability %s: %w", service.ID, grant.Capability, err)
				}
				for key := range usedDisables {
					seenDisabledOperations[key] = true
				}
				for id := range usedPlugins {
					seenDisabledPlugins[id] = true
				}
				grant.Read, grant.Write = operations.read, operations.write
			}
		}
	}
	for _, pluginID := range policy.disabledPluginIDs {
		if !seenDisabledPlugins[pluginID] {
			return PortalPlatformCatalog{}, fmt.Errorf("browserExposure.disabledPlugins 包含未在当前 Platform Profile 中声明浏览器能力的插件 %s", pluginID)
		}
	}
	for _, disable := range policy.disabledOperationIDs {
		if !seenDisabledOperations[disable.key()] {
			return PortalPlatformCatalog{}, fmt.Errorf("browserExposure.disabledOperations 未匹配已声明操作: %s/%s#%s", disable.PluginID, disable.Capability, disable.Operation)
		}
	}
	return ValidateResolvedPortalPlatformCatalog(compiled)
}

func validateBrowserExposurePolicy(policy *BrowserExposurePolicy) error {
	if policy == nil {
		return nil
	}
	seenPlugins := map[string]struct{}{}
	for _, pluginID := range policy.DisabledPlugins {
		if !managementName.MatchString(pluginID) {
			return fmt.Errorf("browserExposure.disabledPlugins 包含无效插件 ID: %s", pluginID)
		}
		if _, duplicate := seenPlugins[pluginID]; duplicate {
			return fmt.Errorf("browserExposure.disabledPlugins 包含重复插件 ID: %s", pluginID)
		}
		seenPlugins[pluginID] = struct{}{}
	}
	seenOperations := map[string]struct{}{}
	for _, disable := range policy.DisabledOperations {
		if !managementName.MatchString(disable.PluginID) || !managementName.MatchString(disable.Capability) || !managementName.MatchString(disable.Operation) {
			return fmt.Errorf("browserExposure.disabledOperations 包含无效操作引用")
		}
		if _, duplicate := seenOperations[disable.key()]; duplicate {
			return fmt.Errorf("browserExposure.disabledOperations 包含重复操作引用: %s/%s#%s", disable.PluginID, disable.Capability, disable.Operation)
		}
		seenOperations[disable.key()] = struct{}{}
	}
	return nil
}

type compiledOperations struct {
	read  []string
	write []string
}

type browserExposurePolicy struct {
	disabledPlugins      map[string]struct{}
	disabledOperations   map[string]struct{}
	disabledPluginIDs    []string
	disabledOperationIDs []BrowserExposureOperationDisable
}

func newBrowserExposurePolicy(value *BrowserExposurePolicy) browserExposurePolicy {
	policy := browserExposurePolicy{disabledPlugins: map[string]struct{}{}, disabledOperations: map[string]struct{}{}}
	if value == nil {
		return policy
	}
	for _, pluginID := range value.DisabledPlugins {
		policy.disabledPlugins[pluginID] = struct{}{}
		policy.disabledPluginIDs = append(policy.disabledPluginIDs, pluginID)
	}
	for _, operation := range value.DisabledOperations {
		policy.disabledOperations[operation.key()] = struct{}{}
		policy.disabledOperationIDs = append(policy.disabledOperationIDs, operation)
	}
	return policy
}

func (v BrowserExposureOperationDisable) key() string {
	return v.PluginID + "\x00" + v.Capability + "\x00" + v.Operation
}

func profileBrowserExposureManifests(profile PlatformProfile, resolve ManifestResolver) (map[string]pluginv1.Manifest, error) {
	refs := append([]PluginRef(nil), profile.Plugins...)
	refs = append(refs, profile.RuntimeEngine.PluginRef, profile.RenderAdapter.PluginRef, profile.Shell.PluginRef, profile.Workbench.PluginRef)
	if profile.AccountCenter != nil {
		refs = append(refs, *profile.AccountCenter)
	}
	manifests := make(map[string]pluginv1.Manifest, len(refs))
	for _, ref := range refs {
		if ref.ID == "" {
			continue
		}
		if existing, found := manifests[ref.ID]; found {
			if existing.Version != ref.Version {
				return nil, fmt.Errorf("Platform Profile 对插件 %s 使用了冲突版本", ref.ID)
			}
			continue
		}
		manifest, err := resolve(ref)
		if err != nil {
			return nil, fmt.Errorf("读取 %s@%s 的已验证 Manifest: %w", ref.ID, ref.Version, err)
		}
		if manifest.ID != ref.ID || manifest.Version != ref.Version {
			return nil, fmt.Errorf("插件引用 %s@%s 与已验证 Manifest %s@%s 不一致", ref.ID, ref.Version, manifest.ID, manifest.Version)
		}
		manifests[ref.ID] = manifest
	}
	return manifests, nil
}

func compileCapabilityExposure(manifests map[string]pluginv1.Manifest, policy browserExposurePolicy, capability string) (compiledOperations, map[string]struct{}, map[string]struct{}, error) {
	read, write := map[string]struct{}{}, map[string]struct{}{}
	seenDisables, seenPlugins := map[string]struct{}{}, map[string]struct{}{}
	ids := make([]string, 0, len(manifests))
	for id := range manifests {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	declared := false
	for _, pluginID := range ids {
		manifest := manifests[pluginID]
		if manifest.Authorization == nil || !manifest.Authorization.BrowserExposed {
			continue
		}
		for _, guard := range manifest.Authorization.OperationGuards {
			if guard.ExtensionPoint != "tool.package" || guard.Capability != capability {
				continue
			}
			declared = true
			if _, disabled := policy.disabledPlugins[pluginID]; disabled {
				seenPlugins[pluginID] = struct{}{}
				continue
			}
			key := BrowserExposureOperationDisable{PluginID: pluginID, Capability: guard.Capability, Operation: guard.Operation}.key()
			if _, disabled := policy.disabledOperations[key]; disabled {
				seenDisables[key] = struct{}{}
				continue
			}
			if guard.Access == "read" {
				if _, conflict := write[guard.Operation]; conflict {
					return compiledOperations{}, nil, nil, fmt.Errorf("不同插件为 operation %s 声明了冲突访问类型", guard.Operation)
				}
				read[guard.Operation] = struct{}{}
				continue
			}
			if _, conflict := read[guard.Operation]; conflict {
				return compiledOperations{}, nil, nil, fmt.Errorf("不同插件为 operation %s 声明了冲突访问类型", guard.Operation)
			}
			write[guard.Operation] = struct{}{}
		}
	}
	if !declared {
		return compiledOperations{}, nil, nil, fmt.Errorf("没有插件以 browserExposed 声明该 capability")
	}
	if len(read)+len(write) == 0 {
		return compiledOperations{}, nil, nil, fmt.Errorf("平台禁用策略移除了该 capability 的全部浏览器操作")
	}
	return compiledOperations{read: sortedKeys(read), write: sortedKeys(write)}, seenDisables, seenPlugins, nil
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func clonePortalPlatformCatalog(in PortalPlatformCatalog) PortalPlatformCatalog {
	out := in
	out.Bindings = make([]PortalBinding, len(in.Bindings))
	for i, binding := range in.Bindings {
		out.Bindings[i] = binding
		out.Bindings[i].Services = make([]ManagedService, len(binding.Services))
		for j, service := range binding.Services {
			out.Bindings[i].Services[j] = service
			out.Bindings[i].Services[j].Capabilities = append([]CapabilityGrant(nil), service.Capabilities...)
			for k := range out.Bindings[i].Services[j].Capabilities {
				out.Bindings[i].Services[j].Capabilities[k].Read = append([]string(nil), service.Capabilities[k].Read...)
				out.Bindings[i].Services[j].Capabilities[k].Write = append([]string(nil), service.Capabilities[k].Write...)
			}
		}
	}
	return out
}
