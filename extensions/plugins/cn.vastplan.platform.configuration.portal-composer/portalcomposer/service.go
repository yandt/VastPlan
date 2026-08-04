// Package portalcomposer implements portal composition governance as a foundation plugin.
package portalcomposer

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	Capability               = portalapi.ComposerCapability
	PlatformCatalogConfigKey = "platform.portal-composer.platformCatalog"
	VersionControlConfigKey  = "platform.portal-composer.versionControl"
)

var (
	ErrForbidden       = portalapi.ErrForbidden
	ErrNotFound        = portalapi.ErrNotFound
	ErrInvalidState    = portalapi.ErrInvalidState
	ErrSelfApproval    = portalapi.ErrSelfApproval
	ErrRouteConflict   = portalapi.ErrRouteConflict
	ErrCatalogRejected = portalapi.ErrCatalogRejected
	ErrStateConflict   = errors.New("Portal Composer Shared State 并发冲突")
)

// Catalog is the trust-aware adapter supplied by the artifact/control plane. A
// plugin may not publish merely because a browser passed a plugin ID.
type Catalog interface {
	ValidatePortal(context.Context, string, portalapi.PortalSpec) error
	MaterializePortal(context.Context, string, portalapi.PortalSpec) ([]pluginv1.ArtifactReference, error)
	PublishReferenceSnapshot(context.Context, pluginv1.ArtifactReferenceSnapshot) error
}

type TestArtifactCatalog interface {
	ValidateTestArtifact(context.Context, string, portalapi.CreateTestReleaseRequest, []string) error
}

type state struct {
	DataFormatVersion         int                                                `json:"dataFormatVersion"`
	NextRevision              uint64                                             `json:"nextRevision"`
	NextGovernance            uint64                                             `json:"nextGovernanceRevision"`
	NextActivation            uint64                                             `json:"nextActivation"`
	NextAudit                 uint64                                             `json:"nextAudit"`
	Revisions                 []portalapi.Revision                               `json:"applications"`
	Profiles                  []portalapi.PlatformProfileRevision                `json:"profiles"`
	Bindings                  []portalapi.BindingRevision                        `json:"bindings"`
	Activations               []portalapi.PortalActivation                       `json:"activations"`
	TestBindings              map[string]portalapi.TestTargetBinding             `json:"testTargetBindings"`
	NextTestRelease           uint64                                             `json:"nextTestRelease"`
	TestReleases              []portalapi.TestRelease                            `json:"testReleases"`
	TestVersionOwners         map[uint64]uint64                                  `json:"testVersionOwners"`
	InstallationVersionOwners map[uint64]string                                  `json:"installationVersionOwners"`
	InstallationPreparations  map[string]portalapi.PluginInstallationPreparation `json:"installationPreparations"`
	Audit                     []portalapi.AuditEvent                             `json:"audit"`
	VersionControls           map[string]portalVersionControlState               `json:"versionControls"`
}

type Service struct {
	mu                         sync.Mutex
	workflowMu                 sync.Mutex
	state                      state
	session                    *composerStateSession
	testSave                   func(state) error
	artifactCatalog            Catalog
	platformCatalog            frontendcompositionv1.PortalPlatformCatalog
	catalogConfigured          bool
	versionControlDefault      *PortalVersionControlBinding
	versionControlConfigLoaded bool
	now                        func() time.Time
	approvalBinding            approvalv2.ProviderBinding
}

func (s *Service) BindPlatformCatalog(catalog frontendcompositionv1.PortalPlatformCatalog) error {
	catalog, err := frontendcompositionv1.ValidateResolvedPortalPlatformCatalog(catalog)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.catalogConfigured {
		if s.platformCatalog.Digest() != catalog.Digest() {
			return errors.New("Portal Platform Catalog 不允许在运行中切换")
		}
		return nil
	}
	s.platformCatalog, s.catalogConfigured = catalog, true
	return nil
}

func New(catalog Catalog) *Service {
	return &Service{state: emptyState(), artifactCatalog: catalog, now: time.Now}
}

func NewWithApprovalBinding(catalog Catalog, binding approvalv2.ProviderBinding) *Service {
	if err := approvalv2.ValidateBinding(binding); err != nil {
		panic("Portal Composer Approval Provider Binding 无效: " + err.Error())
	}
	return &Service{state: emptyState(), artifactCatalog: catalog, approvalBinding: binding, now: time.Now}
}

func (s *Service) BindVersionControl(binding *PortalVersionControlBinding) error {
	if binding != nil {
		value := *binding
		value.EnvironmentID = strings.TrimSpace(value.EnvironmentID)
		value.ResourceType = strings.TrimSpace(value.ResourceType)
		if err := value.validate(); err != nil {
			return err
		}
		binding = &value
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.versionControlConfigLoaded {
		if (s.versionControlDefault == nil) != (binding == nil) || (binding != nil && *s.versionControlDefault != *binding) {
			return errors.New("Portal VersionControl 默认绑定不允许在运行中切换")
		}
		return nil
	}
	s.versionControlDefault = binding
	s.versionControlConfigLoaded = true
	return nil
}

func (s *Service) save() error {
	if s.session == nil {
		if s.testSave != nil {
			return s.testSave(s.state)
		}
		return errors.New("Portal Composer 写入缺少 Shared State 会话")
	}
	revision, err := s.session.repository.save(s.session.ctx, s.session.call, s.state, s.session.revision)
	if err != nil {
		return err
	}
	s.session.revision = revision
	return nil
}

func (s *Service) withTenantState(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant string, work func() error) error {
	if tenant == "" || call == nil || call.GetTenantId() != tenant {
		return ErrForbidden
	}
	s.workflowMu.Lock()
	defer s.workflowMu.Unlock()
	if s.testSave != nil {
		return work()
	}
	repository, err := newComposerStateRepository(host)
	if err != nil {
		return err
	}
	value, revision, err := repository.load(ctx, call)
	if err != nil {
		return err
	}
	if err := validateComposerTenantState(value, tenant); err != nil {
		return err
	}
	s.mu.Lock()
	s.state = value
	s.session = &composerStateSession{ctx: ctx, call: call, repository: repository, tenant: tenant, revision: revision}
	changed := repository.requiresRewrite || s.recoverInterruptedTestReleases()
	var seedErr error
	if changed {
		seedErr = s.save()
	}
	s.mu.Unlock()
	if seedErr != nil {
		s.closeStateSession()
		return seedErr
	}
	defer s.closeStateSession()
	return work()
}

func (s *Service) closeStateSession() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session = nil
	s.state = emptyState()
}

func (s *Service) CreateDraft(ctx context.Context, principal portalapi.Principal, composition frontendcompositionv1.ApplicationComposition) (portalapi.Revision, error) {
	composition, err := frontendcompositionv1.ValidateApplicationComposition(composition)
	if err != nil {
		return portalapi.Revision{}, err
	}
	configuration, err := s.configurationFromCatalog(composition, principal.TenantID)
	if err != nil {
		return portalapi.Revision{}, err
	}
	s.mu.Lock()
	exists := s.portalExistsLocked(principal.TenantID, composition.ID)
	s.mu.Unlock()
	var version portalapi.PortalVersion
	if exists {
		version, err = s.CreatePortalVersion(ctx, principal, composition.ID, configuration)
	} else {
		_, err = s.CreatePortal(ctx, principal, portalapi.CreatePortalRequest{PortalID: composition.ID, Configuration: configuration})
		if err == nil {
			s.mu.Lock()
			index, indexErr := s.workingCopyIndexLocked(principal.TenantID, composition.ID)
			if indexErr == nil {
				version, indexErr = s.portalVersionLocked(principal.TenantID, s.state.Revisions[index])
			}
			s.mu.Unlock()
			err = indexErr
		}
	}
	if err != nil {
		return portalapi.Revision{}, err
	}
	return s.legacyRevision(principal.TenantID, version.ID)
}

func (s *Service) UpdateDraft(ctx context.Context, principal portalapi.Principal, id uint64, composition frontendcompositionv1.ApplicationComposition) (portalapi.Revision, error) {
	composition, err := frontendcompositionv1.ValidateApplicationComposition(composition)
	if err != nil {
		return portalapi.Revision{}, err
	}
	s.mu.Lock()
	i, err := s.revisionIndex(principal.TenantID, id)
	if err != nil {
		s.mu.Unlock()
		return portalapi.Revision{}, err
	}
	version, err := s.portalVersionLocked(principal.TenantID, s.state.Revisions[i])
	s.mu.Unlock()
	if err != nil {
		return portalapi.Revision{}, err
	}
	version.Configuration.Application = composition
	if _, err := s.UpdatePortalVersion(ctx, principal, version.PortalID, id, version.Configuration); err != nil {
		return portalapi.Revision{}, err
	}
	return s.legacyRevision(principal.TenantID, id)
}

func (s *Service) List(_ context.Context, principal portalapi.Principal) ([]portalapi.Revision, error) {
	if principal.TenantID == "" || principal.ID == "" {
		return nil, ErrForbidden
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]portalapi.Revision, 0)
	for _, r := range s.state.Revisions {
		if r.TenantID == principal.TenantID {
			out = append(out, cloneRevision(r))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

func (s *Service) Submit(ctx context.Context, principal portalapi.Principal, id uint64) (portalapi.Revision, error) {
	s.mu.Lock()
	i, err := s.revisionIndex(principal.TenantID, id)
	if err != nil {
		s.mu.Unlock()
		return portalapi.Revision{}, err
	}
	portalID := s.state.Revisions[i].PortalID
	s.mu.Unlock()
	if _, err := s.TransitionPortalVersion(ctx, principal, portalID, id, "submit"); err != nil {
		return portalapi.Revision{}, err
	}
	return s.legacyRevision(principal.TenantID, id)
}

func (s *Service) Approve(ctx context.Context, principal portalapi.Principal, id uint64) (portalapi.Revision, error) {
	s.mu.Lock()
	i, err := s.revisionIndex(principal.TenantID, id)
	if err != nil {
		s.mu.Unlock()
		return portalapi.Revision{}, err
	}
	portalID := s.state.Revisions[i].PortalID
	s.mu.Unlock()
	if _, err := s.TransitionPortalVersion(ctx, principal, portalID, id, "approve"); err != nil {
		return portalapi.Revision{}, err
	}
	return s.legacyRevision(principal.TenantID, id)
}

func (s *Service) Publish(ctx context.Context, principal portalapi.Principal, id uint64, request portalapi.PublishRequest) (portalapi.Revision, error) {
	s.mu.Lock()
	i, err := s.revisionIndex(principal.TenantID, id)
	if err != nil {
		s.mu.Unlock()
		return portalapi.Revision{}, err
	}
	portalID := s.state.Revisions[i].PortalID
	s.mu.Unlock()
	if principal.System {
		if _, err := s.breakGlassPublishPortalVersion(ctx, principal, portalID, id, request.BreakGlassReason); err != nil {
			return portalapi.Revision{}, err
		}
		return s.legacyRevision(principal.TenantID, id)
	}
	if _, err := s.TransitionPortalVersion(ctx, principal, portalID, id, "publish"); err != nil {
		return portalapi.Revision{}, err
	}
	return s.legacyRevision(principal.TenantID, id)
}

func (s *Service) Audit(_ context.Context, principal portalapi.Principal, portalID string, id uint64) ([]portalapi.AuditEvent, error) {
	if principal.TenantID == "" || principal.ID == "" {
		return nil, ErrForbidden
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.revisionIndex(principal.TenantID, id)
	if err != nil {
		return nil, err
	}
	if s.state.Revisions[index].PortalID != portalID || s.isHiddenVersionLocked(id) {
		return nil, ErrNotFound
	}
	out := make([]portalapi.AuditEvent, 0)
	for _, e := range s.state.Audit {
		formalAction := strings.HasPrefix(e.Action, "portal.version.") || strings.HasPrefix(e.Action, "portal.working-copy.") || strings.HasPrefix(e.Action, "portal.publication.")
		if e.TenantID == principal.TenantID && e.RevisionID == id && formalAction {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *Service) revisionIndex(tenant string, id uint64) (int, error) {
	for i := range s.state.Revisions {
		if s.state.Revisions[i].ID == id && s.state.Revisions[i].TenantID == tenant {
			return i, nil
		}
	}
	return 0, ErrNotFound
}
func (s *Service) auditLocked(r portalapi.Revision, action string, p portalapi.Principal, reason, priority string) {
	s.state.NextAudit++
	s.state.Audit = append(s.state.Audit, portalapi.AuditEvent{ID: s.state.NextAudit, TenantID: r.TenantID, PortalID: r.PortalID, RevisionID: r.ID, Action: action, ActorID: p.ID, Reason: reason, Priority: priority, At: s.now().UTC().Format(time.RFC3339Nano)})
}

// requireTrustedPrincipal validates only the identity and tenant established
// by the trusted host. Operation authorization is evaluated once at the
// kernel boundary from the signed Capability Contract; duplicating role names
// here would make online Role/Binding policy ineffective. Domain state,
// ownership and separation-of-duties checks remain in the service.
func requireTrustedPrincipal(p portalapi.Principal) error {
	if p.ID == "" || p.TenantID == "" {
		return ErrForbidden
	}
	return nil
}
func (s *Service) resolveCurrent(composition frontendcompositionv1.ApplicationComposition, tenantID string, revision uint64) (portalapi.PortalSpec, error) {
	if !s.catalogConfigured {
		return portalapi.PortalSpec{}, errors.New("Portal Composer 尚未绑定 Portal Platform Catalog")
	}
	return resolve(s.platformCatalog, composition, tenantID, revision)
}
func cloneSpec(in portalapi.PortalSpec) portalapi.PortalSpec {
	out := in
	out.Domains = append([]string(nil), in.Domains...)
	out.Audience = append([]string(nil), in.Audience...)
	out.Plugins = append([]portalapi.PluginRef(nil), in.Plugins...)
	out.Branding = cloneMap(in.Branding)
	out.Config = cloneMap(in.Config)
	out.RenderAdapter.Config = cloneRenderAdapterConfig(in.RenderAdapter.Config)
	out.Shell.Config = cloneShellConfig(in.Shell.Config)
	out.Management.Services = cloneManagedServices(in.Management.Services)
	out.Resolution.PluginOrigins = cloneStringMap(in.Resolution.PluginOrigins)
	return out
}

func cloneRenderAdapterConfig(in frontendcompositionv1.RenderAdapterConfig) frontendcompositionv1.RenderAdapterConfig {
	out := frontendcompositionv1.RenderAdapterConfig{DefaultRenderer: in.DefaultRenderer, AllowedRenderers: append([]string(nil), in.AllowedRenderers...)}
	if len(in.RendererOptions) != 0 {
		out.RendererOptions = make(map[string]frontendcompositionv1.RendererOptions, len(in.RendererOptions))
		for id, options := range in.RendererOptions {
			out.RendererOptions[id] = options
		}
	}
	out.UserSelectable = in.UserSelectable
	return out
}
func cloneShellConfig(in frontendcompositionv1.ShellConfig) frontendcompositionv1.ShellConfig {
	out := in
	out.NavigationOverrides = make([]frontendcompositionv1.NavigationOverride, len(in.NavigationOverrides))
	for index, override := range in.NavigationOverrides {
		out.NavigationOverrides[index] = override
		if override.Labels != nil {
			out.NavigationOverrides[index].Labels = make(map[string]string, len(override.Labels))
			for locale, label := range override.Labels {
				out.NavigationOverrides[index].Labels[locale] = label
			}
		}
	}
	out.AllowedTemplates = append([]string(nil), in.AllowedTemplates...)
	out.TemplateOptions = make(map[string]map[string]any, len(in.TemplateOptions))
	for template, options := range in.TemplateOptions {
		out.TemplateOptions[template] = cloneMap(options)
	}
	return out
}
func cloneManagedServices(in []frontendcompositionv1.ManagedService) []frontendcompositionv1.ManagedService {
	out := make([]frontendcompositionv1.ManagedService, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].Capabilities = make([]frontendcompositionv1.CapabilityGrant, len(in[i].Capabilities))
		for j := range in[i].Capabilities {
			out[i].Capabilities[j] = in[i].Capabilities[j]
			out[i].Capabilities[j].Read = append([]string(nil), in[i].Capabilities[j].Read...)
			out[i].Capabilities[j].Write = append([]string(nil), in[i].Capabilities[j].Write...)
		}
	}
	return out
}
func cloneComposition(in frontendcompositionv1.ApplicationComposition) frontendcompositionv1.ApplicationComposition {
	out := in
	out.Domains = append([]string(nil), in.Domains...)
	out.Audience = append([]string(nil), in.Audience...)
	out.Plugins = make([]frontendcompositionv1.PluginRef, len(in.Plugins))
	copy(out.Plugins, in.Plugins)
	out.Branding = cloneMap(in.Branding)
	out.Config = cloneMap(in.Config)
	return out
}
func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := map[string]any{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func cloneStringMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func cloneRevision(in portalapi.Revision) portalapi.Revision {
	in.Composition = cloneComposition(in.Composition)
	in.Spec = cloneSpec(in.Spec)
	return in
}
