// Package portalapi defines the stable control-plane contract between Edge/BFF and
// the portal-composer plugin. It deliberately contains no HTTP or UI framework code.
package portalapi

import (
	"context"
	"errors"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
)

var (
	ErrForbidden       = errors.New("没有执行此 Portal 操作的权限")
	ErrNotFound        = errors.New("Portal revision 不存在")
	ErrInvalidState    = errors.New("Portal revision 状态不允许此操作")
	ErrSelfApproval    = errors.New("提交人不能审批自己的草稿")
	ErrRouteConflict   = errors.New("Portal 路由或域名与已发布 Portal 冲突")
	ErrCatalogRejected = errors.New("Portal 制品目录校验失败")
)

// ComposerCapability is the stable tool capability shared by Edge and the
// Portal Composer plugin. Keeping this logical name in the neutral contract
// package prevents the Backend kernel from importing a concrete plugin.
const ComposerCapability = "platform.portal-composer"

// KernelCatalogValidationCapability is the narrowly scoped host capability
// through which the Composer verifies a Portal spec against the trusted
// artifact catalog. It is intentionally not a browser-facing API.
const KernelCatalogValidationCapability = "kernel.portal.catalog.validate"

// KernelCatalogMaterializationCapability is the publish-boundary operation
// that verifies and extracts immutable browser delivery objects before a
// revision can become active.
const KernelCatalogMaterializationCapability = "kernel.portal.catalog.materialize"

type Principal struct {
	ID       string   `json:"id"`
	TenantID string   `json:"tenantId"`
	Roles    []string `json:"roles"`
	System   bool     `json:"system,omitempty"`
}

type PluginRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Channel string `json:"channel,omitempty"`
}

type DesignSystem struct {
	PluginRef
	UIContract string `json:"uiContract"`
}

type ShellComposition struct {
	PluginRef
	UIContract string         `json:"uiContract"`
	Config     map[string]any `json:"config,omitempty"`
}

type ShellLayout struct {
	PluginRef
	UIContract string         `json:"uiContract"`
	Config     map[string]any `json:"config,omitempty"`
}

type PortalSpec struct {
	Revision     uint64                              `json:"revision"`
	ID           string                              `json:"id"`
	TenantID     string                              `json:"tenantId"`
	Route        string                              `json:"route"`
	Domains      []string                            `json:"domains,omitempty"`
	Audience     []string                            `json:"audience,omitempty"`
	Branding     map[string]any                      `json:"branding,omitempty"`
	DesignSystem DesignSystem                        `json:"designSystem"`
	Composition  ShellComposition                    `json:"composition"`
	Layout       ShellLayout                         `json:"layout"`
	Plugins      []PluginRef                         `json:"plugins"`
	Config       map[string]any                      `json:"config,omitempty"`
	Management   frontendcompositionv1.PortalBinding `json:"management"`
	Resolution   Resolution                          `json:"resolution"`
}

type ManagementTarget struct {
	Service frontendcompositionv1.ManagedService
}

func (p PortalSpec) ManagementTarget(serviceID string) (ManagementTarget, bool) {
	if p.Management.TenantID != p.TenantID || p.Management.PortalID != p.ID {
		return ManagementTarget{}, false
	}
	for _, service := range p.Management.Services {
		if service.ID == serviceID {
			return ManagementTarget{Service: service}, true
		}
	}
	return ManagementTarget{}, false
}

func (t ManagementTarget) Allows(capability, operation string, write bool) bool {
	if t.Service.ID == "" || t.Service.LogicalService == "" || t.Service.RoutingDomain == "" {
		return false
	}
	for _, grant := range t.Service.Capabilities {
		if grant.Capability != capability {
			continue
		}
		operations := grant.Read
		if write {
			operations = grant.Write
		}
		for _, candidate := range operations {
			if candidate == operation {
				return true
			}
		}
	}
	return false
}

func (t ManagementTarget) AllowsOperation(capability, operation string) bool {
	return t.Allows(capability, operation, false) || t.Allows(capability, operation, true)
}

type Resolution struct {
	PlatformCatalog         compositioncommonv1.Ref `json:"platformCatalog"`
	PlatformProfile         compositioncommonv1.Ref `json:"platformProfile"`
	ApplicationComposition  compositioncommonv1.Ref `json:"applicationComposition"`
	ManagementBindingDigest string                  `json:"managementBindingDigest"`
	PluginOrigins           map[string]string       `json:"pluginOrigins"`
}

// FrontendModule is an Edge-issued, content-bound browser module descriptor.
// PackageSHA256 proves which verified plugin artifact supplied the module;
// SHA256 binds the exact JavaScript bytes fetched by the browser.
type FrontendModule struct {
	PluginRef
	Entry         string `json:"entry"`
	URL           string `json:"url"`
	SHA256        string `json:"sha256"`
	PackageSHA256 string `json:"packageSha256"`
}

// RuntimeSpec is the only browser bootstrap input. The browser never receives
// raw manifests or repository credentials and does not resolve compositions.
type RuntimeSpec struct {
	Portal  PortalSpec       `json:"portal"`
	Modules []FrontendModule `json:"modules"`
}

type Status string

const (
	StatusDraft           Status = "Draft"
	StatusPendingApproval Status = "PendingApproval"
	StatusApproved        Status = "Approved"
	StatusPublished       Status = "Published"
)

type Revision struct {
	ID          uint64                                       `json:"id"`
	TenantID    string                                       `json:"tenantId"`
	PortalID    string                                       `json:"portalId"`
	Status      Status                                       `json:"status"`
	Active      bool                                         `json:"active"`
	Composition frontendcompositionv1.ApplicationComposition `json:"composition"`
	Spec        PortalSpec                                   `json:"resolved"`
	SubmittedBy string                                       `json:"submittedBy,omitempty"`
	ApprovedBy  string                                       `json:"approvedBy,omitempty"`
	PublishedBy string                                       `json:"publishedBy,omitempty"`
	CreatedAt   string                                       `json:"createdAt"`
	UpdatedAt   string                                       `json:"updatedAt"`
}

type AuditEvent struct {
	ID         uint64 `json:"id"`
	TenantID   string `json:"tenantId"`
	PortalID   string `json:"portalId"`
	RevisionID uint64 `json:"revisionId"`
	Action     string `json:"action"`
	ActorID    string `json:"actorId"`
	Reason     string `json:"reason,omitempty"`
	Priority   string `json:"priority"`
	At         string `json:"at"`
}

type PublishRequest struct {
	BreakGlassReason string `json:"breakGlassReason,omitempty"`
}

// Service is implemented by the configuration/composition plugin and consumed
// through an authenticated Edge adapter. Every method scopes itself to principal.TenantID.
type Service interface {
	CreateDraft(context.Context, Principal, frontendcompositionv1.ApplicationComposition) (Revision, error)
	UpdateDraft(context.Context, Principal, uint64, frontendcompositionv1.ApplicationComposition) (Revision, error)
	List(context.Context, Principal) ([]Revision, error)
	Submit(context.Context, Principal, uint64) (Revision, error)
	Approve(context.Context, Principal, uint64) (Revision, error)
	Publish(context.Context, Principal, uint64, PublishRequest) (Revision, error)
	Rollback(context.Context, Principal, uint64, PublishRequest) (Revision, error)
	Audit(context.Context, Principal, uint64) ([]AuditEvent, error)
}
