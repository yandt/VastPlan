// Package portalapi defines the stable control-plane contract between Edge/BFF and
// the portal-composer plugin. It deliberately contains no HTTP or UI framework code.
package portalapi

import (
	"context"
	"errors"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
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
const ComposerPluginID = "cn.vastplan.platform.configuration.portal-composer"

// KernelCatalogValidationCapability is the narrowly scoped host capability
// through which the Composer verifies a Portal spec against the trusted
// artifact catalog. It is intentionally not a browser-facing API.
const KernelCatalogValidationCapability = "kernel.portal.catalog.validate"

// KernelCatalogMaterializationCapability is the publish-boundary operation
// that verifies and extracts immutable browser delivery objects before a
// revision can become active.
const KernelCatalogMaterializationCapability = "kernel.portal.catalog.materialize"

// KernelArtifactReferencePublicationCapability is the narrow Portal Edge
// bridge to the cluster artifact repository. It accepts only sealed snapshots
// from the authenticated Composer plugin and never exposes a generic router.
const KernelArtifactReferencePublicationCapability = "kernel.portal.artifact-references.publish"

// KernelTestArtifactValidationCapability validates one exact testing receipt
// inside the trusted Portal Edge boundary without exposing repository tokens,
// publisher keys, or artifact bytes to the Composer plugin.
const KernelTestArtifactValidationCapability = "kernel.portal.test-artifact.validate"

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

type RenderAdapter struct {
	PluginRef
	UIContract string                                    `json:"uiContract"`
	Config     frontendcompositionv1.RenderAdapterConfig `json:"config"`
}

type Shell struct {
	PluginRef
	UIContract string                            `json:"uiContract"`
	Config     frontendcompositionv1.ShellConfig `json:"config"`
}

type Workbench struct {
	PluginRef
	UIContract string         `json:"uiContract"`
	Config     map[string]any `json:"config,omitempty"`
}

type PortalSpec struct {
	Revision      uint64                                   `json:"revision"`
	ID            string                                   `json:"id"`
	TenantID      string                                   `json:"tenantId"`
	Route         string                                   `json:"route"`
	Domains       []string                                 `json:"domains,omitempty"`
	Audience      []string                                 `json:"audience,omitempty"`
	Branding      map[string]any                           `json:"branding,omitempty"`
	Localization  frontendcompositionv1.LocalizationPolicy `json:"localization"`
	Updates       frontendcompositionv1.UpdatePolicy       `json:"updates"`
	RenderAdapter RenderAdapter                            `json:"renderAdapter"`
	Shell         Shell                                    `json:"shell"`
	Workbench     Workbench                                `json:"workbench"`
	Plugins       []PluginRef                              `json:"plugins"`
	Config        map[string]any                           `json:"config,omitempty"`
	Management    frontendcompositionv1.PortalBinding      `json:"management"`
	Resolution    Resolution                               `json:"resolution"`
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
	// Deferred modules are locked and authorized like every other module, but
	// must not be preloaded. Renderer selection fetches exactly one on demand.
	Deferred bool `json:"deferred,omitempty"`
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
	Composition frontendcompositionv1.ApplicationComposition `json:"composition"`
	Spec        PortalSpec                                   `json:"resolved"`
	SubmittedBy string                                       `json:"submittedBy,omitempty"`
	ApprovedBy  string                                       `json:"approvedBy,omitempty"`
	PublishedBy string                                       `json:"publishedBy,omitempty"`
	CreatedAt   string                                       `json:"createdAt"`
	UpdatedAt   string                                       `json:"updatedAt"`
}

type PlatformProfileRevision struct {
	ID          uint64                                `json:"id"`
	TenantID    string                                `json:"tenantId"`
	Status      Status                                `json:"status"`
	Profile     frontendcompositionv1.PlatformProfile `json:"profile"`
	SubmittedBy string                                `json:"submittedBy,omitempty"`
	ApprovedBy  string                                `json:"approvedBy,omitempty"`
	PublishedBy string                                `json:"publishedBy,omitempty"`
	CreatedAt   string                                `json:"createdAt"`
	UpdatedAt   string                                `json:"updatedAt"`
}

type BindingRevision struct {
	ID                uint64                              `json:"id"`
	TenantID          string                              `json:"tenantId"`
	PortalID          string                              `json:"portalId"`
	ProfileRevisionID uint64                              `json:"profileRevisionId"`
	Status            Status                              `json:"status"`
	Binding           frontendcompositionv1.PortalBinding `json:"binding"`
	SubmittedBy       string                              `json:"submittedBy,omitempty"`
	ApprovedBy        string                              `json:"approvedBy,omitempty"`
	PublishedBy       string                              `json:"publishedBy,omitempty"`
	CreatedAt         string                              `json:"createdAt"`
	UpdatedAt         string                              `json:"updatedAt"`
}

type ActivationStatus string

const (
	ActivationPreparing  ActivationStatus = "Preparing"
	ActivationActivating ActivationStatus = "Activating"
	ActivationCurrent    ActivationStatus = "Current"
	ActivationSuperseded ActivationStatus = "Superseded"
	ActivationFailed     ActivationStatus = "Failed"
)

type ActivationPhase struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	StartedAt string `json:"startedAt"`
	EndedAt   string `json:"endedAt,omitempty"`
}

// PortalActivation is the immutable live-state fact. Published Application,
// Profile and Binding revisions are only eligible inputs; none is live by itself.
type PortalActivation struct {
	ID                    uint64                       `json:"id"`
	TenantID              string                       `json:"tenantId"`
	PortalID              string                       `json:"portalId"`
	Status                ActivationStatus             `json:"status"`
	ApplicationRevisionID uint64                       `json:"applicationRevisionId"`
	ProfileRevisionID     uint64                       `json:"profileRevisionId"`
	BindingRevisionID     uint64                       `json:"bindingRevisionId"`
	PreviousActivationID  uint64                       `json:"previousActivationId,omitempty"`
	Spec                  PortalSpec                   `json:"resolved"`
	ArtifactReferences    []pluginv1.ArtifactReference `json:"artifactReferences,omitempty"`
	ReferencePending      bool                         `json:"referencePending,omitempty"`
	Phases                []ActivationPhase            `json:"phases"`
	ActorID               string                       `json:"actorId"`
	Reason                string                       `json:"reason,omitempty"`
	CreatedAt             string                       `json:"createdAt"`
}

type ActivationRequest struct {
	PortalID              string `json:"portalId"`
	ApplicationRevisionID uint64 `json:"applicationRevisionId"`
	ProfileRevisionID     uint64 `json:"profileRevisionId"`
	BindingRevisionID     uint64 `json:"bindingRevisionId"`
	ExpectedCurrentID     uint64 `json:"expectedCurrentId"`
	Reason                string `json:"reason,omitempty"`
}

type BindingDraftRequest struct {
	ProfileRevisionID uint64                              `json:"profileRevisionId"`
	Binding           frontendcompositionv1.PortalBinding `json:"binding"`
}

type GovernanceSnapshot struct {
	Profiles     []PlatformProfileRevision `json:"profiles"`
	Applications []Revision                `json:"applications"`
	Bindings     []BindingRevision         `json:"bindings"`
	Activations  []PortalActivation        `json:"activations"`
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

type TestTargetScope string

const (
	TestTargetApplicationPlugin     TestTargetScope = "application-plugin"
	TestTargetPlatformProfilePlugin TestTargetScope = "platform-profile-plugin"
)

// TestTargetBinding authorizes replacement of one plugin slot already owned
// by the current Portal Application. It never authorizes Platform Profile edits.
type TestTargetBinding struct {
	ID                string          `json:"id"`
	TenantID          string          `json:"tenantId"`
	Scope             TestTargetScope `json:"scope"`
	PortalID          string          `json:"portalId"`
	PluginID          string          `json:"pluginId"`
	AllowedPublishers []string        `json:"allowedPublishers"`
	Enabled           bool            `json:"enabled"`
	Version           int64           `json:"version"`
	CreatedAt         string          `json:"createdAt"`
	UpdatedAt         string          `json:"updatedAt"`
}

type PutTestTargetBindingRequest struct {
	Scope             TestTargetScope `json:"scope"`
	PortalID          string          `json:"portalId"`
	PluginID          string          `json:"pluginId"`
	AllowedPublishers []string        `json:"allowedPublishers"`
	Enabled           bool            `json:"enabled"`
	IfVersion         *int64          `json:"ifVersion,omitempty"`
}

type TestReleaseStatus string

const (
	TestReleaseQueued      TestReleaseStatus = "Queued"
	TestReleaseResolving   TestReleaseStatus = "Resolving"
	TestReleasePreparing   TestReleaseStatus = "Preparing"
	TestReleaseValidating  TestReleaseStatus = "Validating"
	TestReleaseActivating  TestReleaseStatus = "Activating"
	TestReleaseReady       TestReleaseStatus = "Ready"
	TestReleaseRollingBack TestReleaseStatus = "RollingBack"
	TestReleaseRolledBack  TestReleaseStatus = "RolledBack"
	TestReleaseFailed      TestReleaseStatus = "Failed"
	TestReleaseSuperseded  TestReleaseStatus = "Superseded"
)

type TestRelease struct {
	ID                             uint64               `json:"id"`
	TenantID                       string               `json:"tenantId"`
	BindingID                      string               `json:"bindingId"`
	Artifact                       pluginv1.ArtifactRef `json:"artifact"`
	SHA256                         string               `json:"sha256"`
	RepositoryRevision             uint64               `json:"repositoryRevision"`
	Status                         TestReleaseStatus    `json:"status"`
	PreviousActivationID           uint64               `json:"previousActivationId,omitempty"`
	CandidateApplicationRevisionID uint64               `json:"candidateApplicationRevisionId,omitempty"`
	CandidateProfileRevisionID     uint64               `json:"candidateProfileRevisionId,omitempty"`
	CandidateBindingRevisionID     uint64               `json:"candidateBindingRevisionId,omitempty"`
	CandidateActivationID          uint64               `json:"candidateActivationId,omitempty"`
	RollbackActivationID           uint64               `json:"rollbackActivationId,omitempty"`
	RollbackRequired               bool                 `json:"rollbackRequired,omitempty"`
	ErrorCode                      string               `json:"errorCode,omitempty"`
	ErrorMessage                   string               `json:"errorMessage,omitempty"`
	RequestedBy                    string               `json:"requestedBy"`
	CreatedAt                      string               `json:"createdAt"`
	UpdatedAt                      string               `json:"updatedAt"`
}

type CreateTestReleaseRequest struct {
	BindingID          string               `json:"bindingId"`
	Artifact           pluginv1.ArtifactRef `json:"artifact"`
	SHA256             string               `json:"sha256"`
	RepositoryRevision uint64               `json:"repositoryRevision"`
}

// TestReleaseService is intentionally separate from Service so consumers that
// only need Portal runtime/governance do not gain test publication authority.
type TestReleaseService interface {
	ListTestTargetBindings(context.Context, Principal) ([]TestTargetBinding, error)
	PutTestTargetBinding(context.Context, Principal, string, PutTestTargetBindingRequest) (TestTargetBinding, error)
	ListTestReleases(context.Context, Principal) ([]TestRelease, error)
	CreateTestRelease(context.Context, Principal, CreateTestReleaseRequest) (TestRelease, error)
	RollbackTestRelease(context.Context, Principal, uint64) (TestRelease, error)
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
	Audit(context.Context, Principal, uint64) ([]AuditEvent, error)
	Governance(context.Context, Principal) (GovernanceSnapshot, error)
	CreateProfileDraft(context.Context, Principal, frontendcompositionv1.PlatformProfile) (PlatformProfileRevision, error)
	UpdateProfileDraft(context.Context, Principal, uint64, frontendcompositionv1.PlatformProfile) (PlatformProfileRevision, error)
	TransitionProfile(context.Context, Principal, uint64, string) (PlatformProfileRevision, error)
	CreateBindingDraft(context.Context, Principal, BindingDraftRequest) (BindingRevision, error)
	UpdateBindingDraft(context.Context, Principal, uint64, BindingDraftRequest) (BindingRevision, error)
	TransitionBinding(context.Context, Principal, uint64, string) (BindingRevision, error)
	Activate(context.Context, Principal, ActivationRequest) (PortalActivation, error)
	RollbackActivation(context.Context, Principal, uint64, uint64, string) (PortalActivation, error)
	ListActivations(context.Context, Principal) ([]PortalActivation, error)
}
