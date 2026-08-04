// Package frontendcompositionv1 defines the two authorized inputs for a
// Frontend Portal composition. Resolved Portal revisions live in portalapi.
package frontendcompositionv1

import (
	_ "embed"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

const (
	PlatformProfileSchemaURL        = "https://schemas.cdsoft.com.cn/vastplan/composition/frontend/v1/vastplan.platform-profile.schema.json"
	ApplicationCompositionSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/composition/frontend/v1/vastplan.application-composition.schema.json"
	PortalPlatformCatalogSchemaURL  = "https://schemas.cdsoft.com.cn/vastplan/composition/frontend/v1/vastplan.portal-platform-catalog.schema.json"
	UIContractSchemaURL             = "https://schemas.cdsoft.com.cn/vastplan/composition/frontend/v1/vastplan.ui-contract.generated.schema.json"
)

//go:embed vastplan.platform-profile.schema.json
var platformSchemaJSON []byte

//go:embed vastplan.application-composition.schema.json
var applicationSchemaJSON []byte

//go:embed vastplan.portal-platform-catalog.schema.json
var portalPlatformCatalogSchemaJSON []byte

//go:embed vastplan.ui-contract.generated.schema.json
var uiContractSchemaJSON []byte

var compileOnce sync.Once
var platformSchema, applicationSchema, portalPlatformCatalogSchema *jsonschema.Schema
var compileErr error

type PluginRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Channel string `json:"channel,omitempty"`
}

// RuntimeEngine selects the single trusted browser/server framework runtime
// for a Portal. It is independent from the visual RenderAdapter: React/Vue are
// engine families while concrete UI frameworks are renderer implementations.
type RuntimeEngine struct {
	PluginRef
	EngineContract string `json:"engineContract"`
	Family         string `json:"family"`
}

type RenderAdapter struct {
	PluginRef
	UIContract string              `json:"uiContract"`
	Config     RenderAdapterConfig `json:"config"`
}

// RenderAdapterConfig governs a framework renderer catalog exposed by one
// trusted adapter plugin. It carries identifiers only, never CSS or framework
// objects.
type RenderAdapterConfig struct {
	DefaultRenderer  string                     `json:"defaultRenderer"`
	AllowedRenderers []string                   `json:"allowedRenderers"`
	UserSelectable   bool                       `json:"userSelectable"`
	RendererOptions  map[string]RendererOptions `json:"rendererOptions,omitempty"`
}

type RendererOptions struct {
	ThemeTemplate         string   `json:"themeTemplate,omitempty"`
	AllowedThemeTemplates []string `json:"allowedThemeTemplates,omitempty"`
	ThemeUserSelectable   bool     `json:"themeUserSelectable,omitempty"`
	IconTheme             string   `json:"iconTheme,omitempty"`
	AllowedIconThemes     []string `json:"allowedIconThemes,omitempty"`
	IconUserSelectable    bool     `json:"iconUserSelectable,omitempty"`
}

// Shell owns the platform-owned semantic page/slot topology and the governed
// catalog of visual templates. Templates may rearrange the stable topology but
// cannot rename or remove slots consumed by functional plugins.
type Shell struct {
	PluginRef
	UIContract string      `json:"uiContract"`
	Config     ShellConfig `json:"config"`
}

type NavigationConfig struct {
	NavigationOverrides []NavigationOverride `json:"navigationOverrides,omitempty"`
}

// NavigationOverride can only adjust a signed plugin-owned navigation node.
// It cannot create a node, enable a plugin, grant permissions or change routes.
type NavigationOverride struct {
	Target string            `json:"target"`
	Hidden *bool             `json:"hidden,omitempty"`
	Order  *int              `json:"order,omitempty"`
	Parent string            `json:"parent,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

type ShellConfig struct {
	NavigationConfig
	DefaultTemplate  string                    `json:"defaultTemplate"`
	AllowedTemplates []string                  `json:"allowedTemplates"`
	UserSelectable   bool                      `json:"userSelectable"`
	TemplateOptions  map[string]map[string]any `json:"templateOptions,omitempty"`
}

// Workbench fixes the governed page workflow runtime. Functional plugins may
// contribute patterns to it but cannot replace it through Application inputs.
type Workbench struct {
	PluginRef
	UIContract string `json:"uiContract"`
	// Config selects governed presentation profiles; functional plugins cannot
	// replace it through Application Composition.
	Config map[string]any `json:"config,omitempty"`
}

type SecurityPolicy struct {
	FirstPartyOnly   bool `json:"firstPartyOnly"`
	RequireIntegrity bool `json:"requireIntegrity"`
}

type LocalizationPolicy struct {
	DefaultLocale    string   `json:"defaultLocale"`
	SupportedLocales []string `json:"supportedLocales"`
}

type UpdatePolicy struct {
	Mode string `json:"mode"`
}

type PlatformProfile struct {
	compositioncommonv1.Document
	Target        compositioncommonv1.Target `json:"target"`
	RuntimeEngine RuntimeEngine              `json:"runtimeEngine"`
	RenderAdapter RenderAdapter              `json:"renderAdapter"`
	Shell         Shell                      `json:"shell"`
	Workbench     Workbench                  `json:"workbench"`
	AccountCenter *PluginRef                 `json:"accountCenter,omitempty"`
	Localization  *LocalizationPolicy        `json:"localization,omitempty"`
	Updates       *UpdatePolicy              `json:"updates,omitempty"`
	Plugins       []PluginRef                `json:"plugins"`
	Security      SecurityPolicy             `json:"security,omitempty"`
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

// PortalPlatformCatalog is the platform-owned seed catalog used to create a
// Portal's first complete version. Profiles and bindings are copied into that
// PortalVersion and are not independently governed online.
type PortalPlatformCatalog struct {
	compositioncommonv1.Document
	Profiles        []PlatformProfile      `json:"profiles"`
	Bindings        []PortalBinding        `json:"bindings"`
	BrowserExposure *BrowserExposurePolicy `json:"browserExposure,omitempty"`
}

// BrowserExposurePolicy is platform-owned configuration for the Portal BFF
// surface. Plugins opt operations in through their signed authorization
// contract; this policy can only remove declared operations. It intentionally
// has no allow-list field, so platform configuration cannot turn an internal
// operation into a browser API.
type BrowserExposurePolicy struct {
	DisabledPlugins    []string                          `json:"disabledPlugins,omitempty"`
	DisabledOperations []BrowserExposureOperationDisable `json:"disabledOperations,omitempty"`
}

type BrowserExposureOperationDisable struct {
	PluginID   string `json:"pluginId"`
	Capability string `json:"capability"`
	Operation  string `json:"operation"`
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
	Resource       *ManagedResource  `json:"resource,omitempty"`
	Capabilities   []CapabilityGrant `json:"capabilities"`
	APIs           []ManagementAPI   `json:"apis,omitempty"`
}

// ManagedResource is an optional, trusted scope carried by a Portal management
// binding. Browser routes may read it but cannot replace it. The first resource
// kind binds self-service operations to one logical backend service unit.
type ManagedResource struct {
	Kind       string `json:"kind"`
	Kernel     string `json:"kernel"`
	Deployment string `json:"deployment"`
	UnitID     string `json:"unitId"`
}

// ManagementAPI binds an opaque Portal-local route id to one exact contract
// already derived from a verified plugin artifact. It deliberately omits the
// plugin id and backend routing target from the browser-visible projection.
type ManagementAPI struct {
	ID              string `json:"id"`
	ContractID      string `json:"contractId"`
	ContractVersion string `json:"contractVersion"`
	ContractDigest  string `json:"contractDigest"`
}

// CapabilityGrant is a capability selection in a source Platform Catalog and
// an exact operation snapshot after trusted compilation. Source catalogs must
// not populate Read or Write: those fields are derived solely from signed
// plugin authorization contracts. The runtime keeps the compiled arrays so
// every BFF invocation remains fail-closed against its immutable Generation.
type CapabilityGrant struct {
	Capability string   `json:"capability"`
	Read       []string `json:"read,omitempty"`
	Write      []string `json:"write,omitempty"`
}
