package authenticationv1

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

func ParseAuthenticationProviderProfile(raw []byte) (AuthenticationProviderProfile, error) {
	if len(raw) > MaxProviderProfileBytes {
		return AuthenticationProviderProfile{}, errors.New("Authentication Provider Profile 超过大小上限")
	}
	if err := validateSchema(ProviderSchemaURL, raw); err != nil {
		return AuthenticationProviderProfile{}, err
	}
	var profile AuthenticationProviderProfile
	if err := decodeStrict(raw, &profile); err != nil {
		return AuthenticationProviderProfile{}, err
	}
	normalizeProviderProfile(&profile)
	if err := validateProviderProfile(profile); err != nil {
		return AuthenticationProviderProfile{}, err
	}
	return profile, nil
}

func ParseAuthenticationProviderCatalog(raw []byte) (AuthenticationProviderCatalog, error) {
	if len(raw) > MaxProviderCatalogBytes {
		return AuthenticationProviderCatalog{}, errors.New("Authentication Provider Catalog 超过大小上限")
	}
	if err := validateSchema(ProviderSchemaURL+"#/$defs/providerCatalog", raw); err != nil {
		return AuthenticationProviderCatalog{}, err
	}
	var catalog AuthenticationProviderCatalog
	if err := decodeStrict(raw, &catalog); err != nil {
		return AuthenticationProviderCatalog{}, err
	}
	normalizeProviderCatalog(&catalog)
	if err := validateProviderCatalog(catalog); err != nil {
		return AuthenticationProviderCatalog{}, err
	}
	return catalog, nil
}

func ParseAuthenticationProviderLifecycle(raw []byte) (AuthenticationProviderLifecycle, error) {
	if len(raw) > MaxProviderProfileBytes {
		return AuthenticationProviderLifecycle{}, errors.New("Authentication Provider Lifecycle 超过大小上限")
	}
	if err := validateSchema(ProviderSchemaURL+"#/$defs/providerLifecycle", raw); err != nil {
		return AuthenticationProviderLifecycle{}, err
	}
	var lifecycle AuthenticationProviderLifecycle
	if err := decodeStrict(raw, &lifecycle); err != nil {
		return AuthenticationProviderLifecycle{}, err
	}
	sort.Strings(lifecycle.UnmetCapabilities)
	if lifecycle.Readiness == ProviderReady && len(lifecycle.UnmetCapabilities) != 0 {
		return AuthenticationProviderLifecycle{}, errors.New("Ready Provider 不得存在未满足能力")
	}
	if lifecycle.Readiness == ProviderBlocked && len(lifecycle.UnmetCapabilities) == 0 {
		return AuthenticationProviderLifecycle{}, errors.New("Blocked Provider 必须声明未满足能力")
	}
	return lifecycle, nil
}

func (profile AuthenticationProviderProfile) Digest() string {
	clone := profile
	normalizeProviderProfile(&clone)
	return digestJSON(clone)
}

func (catalog AuthenticationProviderCatalog) Digest() string {
	clone := catalog
	clone.Providers = append([]ProviderCatalogEntry(nil), catalog.Providers...)
	clone.Bindings = append([]ProviderBinding(nil), catalog.Bindings...)
	normalizeProviderCatalog(&clone)
	return digestJSON(clone)
}

func normalizeProviderProfile(profile *AuthenticationProviderProfile) {
	profile.ContributionID = strings.TrimSpace(profile.ContributionID)
	profile.SubjectNamespace = strings.TrimSpace(profile.SubjectNamespace)
	sort.Slice(profile.Purposes, func(i, j int) bool { return profile.Purposes[i] < profile.Purposes[j] })
	sort.Strings(profile.Methods)
	sort.Strings(profile.RequiredCapabilities)
}

func normalizeProviderCatalog(catalog *AuthenticationProviderCatalog) {
	for index := range catalog.Providers {
		provider := &catalog.Providers[index]
		provider.ContributionID = strings.TrimSpace(provider.ContributionID)
		provider.SubjectNamespace = strings.TrimSpace(provider.SubjectNamespace)
		sort.Slice(provider.Purposes, func(i, j int) bool { return provider.Purposes[i] < provider.Purposes[j] })
		sort.Strings(provider.Methods)
		sort.Strings(provider.RequiredCapabilities)
	}
	for index := range catalog.Bindings {
		sort.Strings(catalog.Bindings[index].AllowedProviders)
	}
	sort.Slice(catalog.Providers, func(i, j int) bool { return catalog.Providers[i].Profile.ID < catalog.Providers[j].Profile.ID })
	sort.Slice(catalog.Bindings, func(i, j int) bool {
		if catalog.Bindings[i].TenantID != catalog.Bindings[j].TenantID {
			return catalog.Bindings[i].TenantID < catalog.Bindings[j].TenantID
		}
		return catalog.Bindings[i].PortalID < catalog.Bindings[j].PortalID
	})
}

func validateProviderProfile(profile AuthenticationProviderProfile) error {
	if hasDuplicatePurposes(profile.Purposes) || hasDuplicateStrings(profile.Methods) || hasDuplicateStrings(profile.RequiredCapabilities) {
		return errors.New("Authentication Provider Profile 的 purposes、methods 和 requiredCapabilities 必须唯一")
	}
	return nil
}

func validateProviderCatalog(catalog AuthenticationProviderCatalog) error {
	providers := make(map[string]ProviderCatalogEntry, len(catalog.Providers))
	for _, provider := range catalog.Providers {
		if _, exists := providers[provider.Profile.ID]; exists {
			return fmt.Errorf("Authentication Provider Profile 重复: %s", provider.Profile.ID)
		}
		providers[provider.Profile.ID] = provider
	}
	bindings := map[string]struct{}{}
	for _, binding := range catalog.Bindings {
		key := binding.TenantID + "\x00" + binding.PortalID
		if _, exists := bindings[key]; exists {
			return fmt.Errorf("Authentication Provider Binding 重复: %s/%s", binding.TenantID, binding.PortalID)
		}
		bindings[key] = struct{}{}
		if !containsString(binding.AllowedProviders, binding.DefaultProvider) {
			return fmt.Errorf("Binding %s/%s 的 defaultProvider 必须在 allowedProviders 中", binding.TenantID, binding.PortalID)
		}
		methods := map[string]string{}
		for _, providerID := range binding.AllowedProviders {
			provider, exists := providers[providerID]
			if !exists {
				return fmt.Errorf("Binding %s/%s 引用了未知 Provider %s", binding.TenantID, binding.PortalID, providerID)
			}
			for _, methodID := range provider.Methods {
				if owner, ambiguous := methods[methodID]; ambiguous {
					return fmt.Errorf("Binding %s/%s 的认证方式 %s 同时由 %s 和 %s 提供", binding.TenantID, binding.PortalID, methodID, owner, providerID)
				}
				methods[methodID] = providerID
			}
		}
	}
	return nil
}

func hasDuplicateStrings(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

func hasDuplicatePurposes(values []ProviderPurpose) bool {
	seen := make(map[ProviderPurpose]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}
