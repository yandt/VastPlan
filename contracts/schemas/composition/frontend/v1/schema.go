// Package frontendcompositionv1 defines the two authorized inputs for a
// Frontend Portal composition. Resolved Portal revisions live in portalapi.
package frontendcompositionv1

import (
	"bytes"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

const (
	PlatformProfileSchemaURL        = "https://schemas.cdsoft.com.cn/vastplan/composition/frontend/v1/vastplan.platform-profile.schema.json"
	ApplicationCompositionSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/composition/frontend/v1/vastplan.application-composition.schema.json"
	PortalPlatformCatalogSchemaURL  = "https://schemas.cdsoft.com.cn/vastplan/composition/frontend/v1/vastplan.portal-platform-catalog.schema.json"
)

//go:embed vastplan.platform-profile.schema.json
var platformSchemaJSON []byte

//go:embed vastplan.application-composition.schema.json
var applicationSchemaJSON []byte

//go:embed vastplan.portal-platform-catalog.schema.json
var portalPlatformCatalogSchemaJSON []byte

var compileOnce sync.Once
var platformSchema, applicationSchema, portalPlatformCatalogSchema *jsonschema.Schema
var compileErr error

type PluginRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Channel string `json:"channel,omitempty"`
}

type DesignSystem struct {
	PluginRef
	UIContract string `json:"uiContract"`
}

// ShellComposition fixes the platform-owned semantic page and slot topology.
// It is deliberately separate from ShellLayout so changing CSS/layout cannot
// silently rename or remove extension slots consumed by functional plugins.
type ShellComposition struct {
	PluginRef
	UIContract string         `json:"uiContract"`
	Config     map[string]any `json:"config,omitempty"`
}

// ShellLayout selects the visual arrangement for an already normalized shell
// composition. Config is layout-private, non-sensitive JSON owned by the
// Platform Profile rather than an Application Composition.
type ShellLayout struct {
	PluginRef
	UIContract string         `json:"uiContract"`
	Config     map[string]any `json:"config,omitempty"`
}

type SecurityPolicy struct {
	FirstPartyOnly   bool `json:"firstPartyOnly"`
	RequireIntegrity bool `json:"requireIntegrity"`
}

type LocalizationPolicy struct {
	DefaultLocale    string   `json:"defaultLocale"`
	SupportedLocales []string `json:"supportedLocales"`
}

type PlatformProfile struct {
	compositioncommonv1.Document
	Target       compositioncommonv1.Target `json:"target"`
	DesignSystem DesignSystem               `json:"designSystem"`
	Composition  ShellComposition           `json:"composition"`
	Layout       ShellLayout                `json:"layout"`
	Localization *LocalizationPolicy        `json:"localization,omitempty"`
	Plugins      []PluginRef                `json:"plugins"`
	Security     SecurityPolicy             `json:"security,omitempty"`
}

type ApplicationComposition struct {
	compositioncommonv1.Document
	Target   compositioncommonv1.Target `json:"target"`
	Route    string                     `json:"route"`
	Domains  []string                   `json:"domains,omitempty"`
	Audience []string                   `json:"audience,omitempty"`
	Branding map[string]any             `json:"branding,omitempty"`
	Plugins  []PluginRef                `json:"plugins"`
	Config   map[string]any             `json:"config,omitempty"`
}

// PortalPlatformCatalog is the platform-owned authority for selecting a shared
// Frontend Platform Profile and granting a portal access to exact backend
// logical services. Application Composition cannot alter these bindings.
type PortalPlatformCatalog struct {
	compositioncommonv1.Document
	Profiles []PlatformProfile `json:"profiles"`
	Bindings []PortalBinding   `json:"bindings"`
}

type PortalBinding struct {
	TenantID        string                  `json:"tenantId"`
	PortalID        string                  `json:"portalId"`
	PlatformProfile compositioncommonv1.Ref `json:"platformProfile"`
	Services        []ManagedService        `json:"services"`
}

// ManagedService.ID is the only browser-visible selector. The BFF resolves it
// to the exact logicalService/routingDomain pair and never accepts either
// routing field directly from a browser.
type ManagedService struct {
	ID             string            `json:"id"`
	Label          string            `json:"label,omitempty"`
	LogicalService string            `json:"logicalService"`
	RoutingDomain  string            `json:"routingDomain"`
	Capabilities   []CapabilityGrant `json:"capabilities"`
}

// CapabilityGrant separates read and write operations so a read-only portal
// cannot gain mutation authority merely because a new HTTP route is added.
type CapabilityGrant struct {
	Capability string   `json:"capability"`
	Read       []string `json:"read,omitempty"`
	Write      []string `json:"write,omitempty"`
}

func schemas() (*jsonschema.Schema, *jsonschema.Schema, *jsonschema.Schema, error) {
	compileOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		if err := commonv1.AddResources(compiler); err != nil {
			compileErr = err
			return
		}
		if err := compositioncommonv1.AddResources(compiler); err != nil {
			compileErr = err
			return
		}
		for _, resource := range []struct {
			url string
			raw []byte
		}{{PlatformProfileSchemaURL, platformSchemaJSON}, {ApplicationCompositionSchemaURL, applicationSchemaJSON}, {PortalPlatformCatalogSchemaURL, portalPlatformCatalogSchemaJSON}} {
			doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(resource.raw))
			if err != nil {
				compileErr = err
				return
			}
			if err := compiler.AddResource(resource.url, doc); err != nil {
				compileErr = err
				return
			}
		}
		platformSchema, compileErr = compiler.Compile(PlatformProfileSchemaURL)
		if compileErr != nil {
			return
		}
		applicationSchema, compileErr = compiler.Compile(ApplicationCompositionSchemaURL)
		if compileErr != nil {
			return
		}
		portalPlatformCatalogSchema, compileErr = compiler.Compile(PortalPlatformCatalogSchemaURL)
	})
	return platformSchema, applicationSchema, portalPlatformCatalogSchema, compileErr
}

func ParsePlatformProfile(raw []byte) (PlatformProfile, error) {
	p, _, _, err := schemas()
	if err != nil {
		return PlatformProfile{}, err
	}
	if err := validateJSON(p, raw, "Frontend Platform Profile"); err != nil {
		return PlatformProfile{}, err
	}
	var value PlatformProfile
	if err := json.Unmarshal(raw, &value); err != nil {
		return PlatformProfile{}, err
	}
	if err := compositioncommonv1.ValidateTarget(value.Target, compositioncommonv1.KernelFrontend); err != nil {
		return PlatformProfile{}, err
	}
	value.Plugins, err = normalizeRefs(value.Plugins)
	if err != nil {
		return PlatformProfile{}, err
	}
	value.DesignSystem.Channel = channel(value.DesignSystem.Channel)
	value.Composition.Channel = channel(value.Composition.Channel)
	value.Layout.Channel = channel(value.Layout.Channel)
	if value.Localization != nil && !containsFold(value.Localization.SupportedLocales, value.Localization.DefaultLocale) {
		return PlatformProfile{}, fmt.Errorf("Frontend Platform Profile 默认语言必须包含在 supportedLocales 中")
	}
	if value.DesignSystem.ID == value.Composition.ID || value.DesignSystem.ID == value.Layout.ID || value.Composition.ID == value.Layout.ID {
		return PlatformProfile{}, fmt.Errorf("设计系统、Shell 组合与布局必须由三个独立插件提供")
	}
	found := map[string]bool{}
	for _, ref := range value.Plugins {
		for _, selected := range []PluginRef{value.DesignSystem.PluginRef, value.Composition.PluginRef, value.Layout.PluginRef} {
			if same(ref, selected) {
				found[selected.ID] = true
			}
		}
	}
	if !found[value.DesignSystem.ID] || !found[value.Composition.ID] || !found[value.Layout.ID] {
		return PlatformProfile{}, fmt.Errorf("Frontend Platform Profile plugins 必须精确包含设计系统、Shell 组合与布局插件")
	}
	return value, nil
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

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
	raw, err := os.ReadFile(path)
	if err != nil {
		return PlatformProfile{}, err
	}
	return ParsePlatformProfile(raw)
}
func ParseApplicationCompositionFile(path string) (ApplicationComposition, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ApplicationComposition{}, err
	}
	return ParseApplicationComposition(raw)
}
func ParsePortalPlatformCatalogFile(path string) (PortalPlatformCatalog, error) {
	raw, err := os.ReadFile(path)
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
