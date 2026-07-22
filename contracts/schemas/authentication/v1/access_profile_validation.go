package authenticationv1

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

func ParseAccessProfile(raw []byte) (AccessProfile, error) {
	if len(raw) > MaxAccessProfileBytes {
		return AccessProfile{}, errors.New("Access Profile 超过大小上限")
	}
	if err := validateSchema(AccessSchemaURL, raw); err != nil {
		return AccessProfile{}, err
	}
	var profile AccessProfile
	if err := decodeStrict(raw, &profile); err != nil {
		return AccessProfile{}, err
	}
	normalizeAccessProfile(&profile)
	if err := validateAccessProfile(profile); err != nil {
		return AccessProfile{}, err
	}
	return profile, nil
}

func ParseAccessProfileCatalog(raw []byte) (AccessProfileCatalog, error) {
	if len(raw) > MaxAccessCatalogBytes {
		return AccessProfileCatalog{}, errors.New("Access Profile Catalog 超过大小上限")
	}
	if err := validateSchema(AccessSchemaURL+"#/$defs/accessProfileCatalog", raw); err != nil {
		return AccessProfileCatalog{}, err
	}
	var catalog AccessProfileCatalog
	if err := decodeStrict(raw, &catalog); err != nil {
		return AccessProfileCatalog{}, err
	}
	for index := range catalog.Profiles {
		normalizeAccessProfile(&catalog.Profiles[index])
	}
	sort.Slice(catalog.Profiles, func(i, j int) bool { return catalog.Profiles[i].ID < catalog.Profiles[j].ID })
	if err := validateAccessCatalog(catalog); err != nil {
		return AccessProfileCatalog{}, err
	}
	return catalog, nil
}

func ParseAccessBootstrap(raw []byte) (AccessBootstrap, error) {
	if len(raw) > MaxAccessProfileBytes {
		return AccessBootstrap{}, errors.New("Access Bootstrap 超过大小上限")
	}
	if err := validateSchema(AccessSchemaURL+"#/$defs/accessBootstrap", raw); err != nil {
		return AccessBootstrap{}, err
	}
	var bootstrap AccessBootstrap
	if err := decodeStrict(raw, &bootstrap); err != nil {
		return AccessBootstrap{}, err
	}
	if err := validateAccessLocalization(bootstrap.Localization); err != nil {
		return AccessBootstrap{}, err
	}
	if !contains(bootstrap.Authentication.AllowedMethods, bootstrap.Authentication.DefaultMethod) {
		return AccessBootstrap{}, errors.New("Access Bootstrap defaultMethod 必须包含在 allowedMethods 中")
	}
	if err := validateLocalizedText(bootstrap.Branding.ProductName); err != nil {
		return AccessBootstrap{}, fmt.Errorf("Access Bootstrap branding: %w", err)
	}
	for _, path := range []string{bootstrap.Branding.SupportPath, bootstrap.Branding.PrivacyPath} {
		if path != "" && !validLocalPath(path) {
			return AccessBootstrap{}, errors.New("Access Bootstrap 支持与隐私路径必须是安全同源路径")
		}
	}
	return bootstrap, nil
}

func (profile AccessProfile) Digest() string {
	clone := cloneAccessProfile(profile)
	normalizeAccessProfile(&clone)
	return digestJSON(clone)
}

func (catalog AccessProfileCatalog) Digest() string {
	profiles := make([]AccessProfile, len(catalog.Profiles))
	for index, profile := range catalog.Profiles {
		profiles[index] = cloneAccessProfile(profile)
		normalizeAccessProfile(&profiles[index])
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ID < profiles[j].ID })
	clone := catalog
	clone.Profiles = profiles
	return digestJSON(clone)
}

func cloneAccessProfile(profile AccessProfile) AccessProfile {
	clone := profile
	clone.Domains = append([]string(nil), profile.Domains...)
	clone.Authentication.AllowedMethods = append([]string(nil), profile.Authentication.AllowedMethods...)
	clone.Localization.SupportedLocales = append([]string(nil), profile.Localization.SupportedLocales...)
	return clone
}

func digestJSON(value any) string {
	raw, _ := json.Marshal(value)
	digest := sha256.Sum256(raw)
	return fmt.Sprintf("%x", digest)
}

func normalizeAccessProfile(profile *AccessProfile) {
	for index := range profile.Domains {
		profile.Domains[index] = strings.ToLower(strings.TrimSuffix(profile.Domains[index], "."))
	}
	sort.Strings(profile.Domains)
}

func validateAccessProfile(profile AccessProfile) error {
	if err := validateLocalizedText(profile.Branding.ProductName); err != nil {
		return fmt.Errorf("Access Profile branding: %w", err)
	}
	if !validLocalPath(profile.Route) || strings.ContainsAny(profile.Route, "?#") || (profile.Route != "/" && strings.HasSuffix(profile.Route, "/")) {
		return errors.New("Access Profile route 必须是无 query/fragment/backslash 的规范同源路径")
	}
	for _, path := range []string{profile.Branding.SupportPath, profile.Branding.PrivacyPath} {
		if path != "" && !validLocalPath(path) {
			return errors.New("Access Profile 支持与隐私路径必须是安全同源路径")
		}
	}
	if !contains(profile.Authentication.AllowedMethods, profile.Authentication.DefaultMethod) {
		return errors.New("Access Profile defaultMethod 必须包含在 allowedMethods 中")
	}
	if err := validateAccessLocalization(profile.Localization); err != nil {
		return fmt.Errorf("Access Profile localization: %w", err)
	}
	if (profile.Branding.LogoAssetID == "") != (profile.Branding.LogoSHA256 == "") {
		return errors.New("Access Profile logoAssetId 与 logoSha256 必须同时出现")
	}
	return nil
}

func validateAccessLocalization(policy AccessLocalizationPolicy) error {
	for _, locale := range policy.SupportedLocales {
		if strings.EqualFold(locale, policy.DefaultLocale) {
			return nil
		}
	}
	return errors.New("defaultLocale 必须包含在 supportedLocales 中")
}

func validLocalPath(value string) bool {
	return strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") && !strings.Contains(value, "\\") && !strings.ContainsAny(value, "\x00\r\n")
}

func validateAccessCatalog(catalog AccessProfileCatalog) error {
	profileIDs := map[string]struct{}{}
	routes := map[string]string{}
	for _, profile := range catalog.Profiles {
		if err := validateAccessProfile(profile); err != nil {
			return fmt.Errorf("Access Profile %s 无效: %w", profile.ID, err)
		}
		if _, duplicate := profileIDs[profile.ID]; duplicate {
			return fmt.Errorf("Access Profile ID 重复: %s", profile.ID)
		}
		profileIDs[profile.ID] = struct{}{}
		for _, domain := range profile.Domains {
			key := domain + "\x00" + profile.Route
			if owner, duplicate := routes[key]; duplicate {
				return fmt.Errorf("Access Profile 路由冲突 %s%s: %s 与 %s", domain, profile.Route, owner, profile.ID)
			}
			routes[key] = profile.ID
		}
	}
	return nil
}
