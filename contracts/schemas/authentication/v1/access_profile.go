package authenticationv1

import (
	"strings"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

type AccessBranding struct {
	ProductName LocalizedText `json:"productName"`
	LogoAssetID string        `json:"logoAssetId,omitempty"`
	LogoSHA256  string        `json:"logoSha256,omitempty"`
	SupportPath string        `json:"supportPath,omitempty"`
	PrivacyPath string        `json:"privacyPath,omitempty"`
}

type PublicAccessBranding struct {
	ProductName LocalizedText `json:"productName"`
	LogoAssetID string        `json:"logoAssetId,omitempty"`
	SupportPath string        `json:"supportPath,omitempty"`
	PrivacyPath string        `json:"privacyPath,omitempty"`
}

type AccessMethodPolicy struct {
	AllowedMethods  []string `json:"allowedMethods"`
	DefaultMethod   string   `json:"defaultMethod"`
	ReuseIdentifier bool     `json:"reuseIdentifier"`
}

// AccessLocalizationPolicy is deliberately scoped to the public, pre-session
// experience. It does not replace the localization policy of the referenced
// Frontend Platform Profile.
type AccessLocalizationPolicy struct {
	DefaultLocale    string   `json:"defaultLocale"`
	SupportedLocales []string `json:"supportedLocales"`
}

// AccessProfile is selected before a user exists. It references an immutable
// Frontend Platform Profile instead of duplicating Runtime/Renderer/Shell/
// Workbench choices.
type AccessProfile struct {
	compositioncommonv1.Document
	TenantID        string                   `json:"tenantId"`
	PortalID        string                   `json:"portalId"`
	Route           string                   `json:"route"`
	Domains         []string                 `json:"domains"`
	PlatformProfile compositioncommonv1.Ref  `json:"platformProfile"`
	AccessTemplate  string                   `json:"accessTemplate"`
	Localization    AccessLocalizationPolicy `json:"localization"`
	Authentication  AccessMethodPolicy       `json:"authentication"`
	Branding        AccessBranding           `json:"branding"`
}

type AccessProfileCatalog struct {
	compositioncommonv1.Document
	Profiles []AccessProfile `json:"profiles"`
}

// AccessBootstrap is the browser-safe projection of an immutable pre-session
// generation. It intentionally excludes tenant, Portal and Platform Profile
// references.
type AccessBootstrap struct {
	SchemaVersion  string                   `json:"schemaVersion"`
	GenerationID   string                   `json:"generationId"`
	AccessTemplate string                   `json:"accessTemplate"`
	Localization   AccessLocalizationPolicy `json:"localization"`
	Authentication AccessMethodPolicy       `json:"authentication"`
	Branding       PublicAccessBranding     `json:"branding"`
}

func (catalog AccessProfileCatalog) Resolve(host, path string) (AccessProfile, bool) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	var selected AccessProfile
	found := false
	for _, profile := range catalog.Profiles {
		if !containsString(profile.Domains, host) || !routeMatches(profile.Route, path) {
			continue
		}
		if !found || len(profile.Route) > len(selected.Route) {
			selected, found = profile, true
		}
	}
	return selected, found
}

func routeMatches(route, path string) bool {
	return route == "/" || path == route || strings.HasPrefix(path, route+"/")
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
