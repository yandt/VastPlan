// Package portalcomposer implements portal composition governance as a foundation plugin.
package portalcomposer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

const (
	PluginID      = "com.vastplan.platform.configuration.portal-composer"
	PluginVersion = "1.0.0"
	Capability    = portalapi.ComposerCapability
	// StateFileConfigKey is read only through the authenticated host callback;
	// plugin process environment must not decide where governed state is stored.
	StateFileConfigKey       = "platform.portal-composer.stateFile"
	PlatformCatalogConfigKey = "platform.portal-composer.platformCatalog"
)

var (
	ErrForbidden       = portalapi.ErrForbidden
	ErrNotFound        = portalapi.ErrNotFound
	ErrInvalidState    = portalapi.ErrInvalidState
	ErrSelfApproval    = portalapi.ErrSelfApproval
	ErrRouteConflict   = portalapi.ErrRouteConflict
	ErrCatalogRejected = portalapi.ErrCatalogRejected
)

// Catalog is the trust-aware adapter supplied by the artifact/control plane. A
// plugin may not publish merely because a browser passed a plugin ID.
type Catalog interface {
	ValidatePortal(context.Context, string, portalapi.PortalSpec) error
	MaterializePortal(context.Context, string, portalapi.PortalSpec) error
}

type state struct {
	NextRevision uint64                 `json:"nextRevision"`
	NextAudit    uint64                 `json:"nextAudit"`
	Revisions    []portalapi.Revision   `json:"revisions"`
	Audit        []portalapi.AuditEvent `json:"audit"`
}

type Service struct {
	mu                sync.Mutex
	state             state
	stateFile         string
	artifactCatalog   Catalog
	platformCatalog   frontendcompositionv1.PortalPlatformCatalog
	catalogConfigured bool
	now               func() time.Time
}

func (s *Service) BindPlatformCatalog(catalog frontendcompositionv1.PortalPlatformCatalog) error {
	catalog, err := frontendcompositionv1.ValidatePortalPlatformCatalog(catalog)
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

func New(stateFile string, catalog Catalog) (*Service, error) {
	s := &Service{artifactCatalog: catalog, now: time.Now}
	if strings.TrimSpace(stateFile) != "" {
		if err := s.configure(stateFile); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// configure is intentionally one-way: a running plugin cannot be pointed at
// another state file by a later call or by a tenant-controlled request.
func (s *Service) configure(stateFile string) error {
	if strings.TrimSpace(stateFile) == "" {
		return errors.New("Portal Composer stateFile 不能为空")
	}
	if s.stateFile != "" && s.stateFile != stateFile {
		return errors.New("Portal Composer stateFile 不允许在运行中切换")
	}
	if s.stateFile != "" {
		return nil
	}
	s.stateFile = stateFile
	return s.load()
}

func (s *Service) load() error {
	raw, err := os.ReadFile(s.stateFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取 Portal Composer 状态: %w", err)
	}
	if err := json.Unmarshal(raw, &s.state); err != nil {
		return fmt.Errorf("解析 Portal Composer 状态: %w", err)
	}
	return nil
}

func (s *Service) save() error {
	if s.stateFile == "" {
		return errors.New("Portal Composer 尚未配置状态文件")
	}
	raw, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.stateFile), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.stateFile), ".portal-composer-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.stateFile)
}

func (s *Service) CreateDraft(ctx context.Context, principal portalapi.Principal, composition frontendcompositionv1.ApplicationComposition) (portalapi.Revision, error) {
	if err := require(principal, "portal.compose"); err != nil {
		return portalapi.Revision{}, err
	}
	composition, err := frontendcompositionv1.ValidateApplicationComposition(composition)
	if err != nil {
		return portalapi.Revision{}, err
	}
	preview, err := s.resolveCurrent(composition, principal.TenantID, 1)
	if err != nil {
		return portalapi.Revision{}, err
	}
	if err := s.validateCatalog(ctx, principal.TenantID, preview); err != nil {
		return portalapi.Revision{}, fmt.Errorf("%w: %v", ErrCatalogRejected, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC().Format(time.RFC3339Nano)
	s.state.NextRevision++
	resolved, err := s.resolveCurrent(composition, principal.TenantID, s.state.NextRevision)
	if err != nil {
		return portalapi.Revision{}, err
	}
	r := portalapi.Revision{ID: s.state.NextRevision, TenantID: principal.TenantID, PortalID: composition.ID, Status: portalapi.StatusDraft, Composition: cloneComposition(composition), Spec: cloneSpec(resolved), CreatedAt: now, UpdatedAt: now}
	s.state.Revisions = append(s.state.Revisions, r)
	s.auditLocked(r, "draft.created", principal, "", "normal")
	if err := s.save(); err != nil {
		return portalapi.Revision{}, err
	}
	return r, nil
}

func (s *Service) UpdateDraft(ctx context.Context, principal portalapi.Principal, id uint64, composition frontendcompositionv1.ApplicationComposition) (portalapi.Revision, error) {
	if err := require(principal, "portal.compose"); err != nil {
		return portalapi.Revision{}, err
	}
	composition, err := frontendcompositionv1.ValidateApplicationComposition(composition)
	if err != nil {
		return portalapi.Revision{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.revisionIndex(principal.TenantID, id)
	if err != nil {
		return portalapi.Revision{}, err
	}
	r := &s.state.Revisions[i]
	if r.Status != portalapi.StatusDraft {
		return portalapi.Revision{}, ErrInvalidState
	}
	resolved, err := s.resolveCurrent(composition, principal.TenantID, r.ID)
	if err != nil {
		return portalapi.Revision{}, err
	}
	if err := s.validateCatalog(ctx, principal.TenantID, resolved); err != nil {
		return portalapi.Revision{}, fmt.Errorf("%w: %v", ErrCatalogRejected, err)
	}
	r.PortalID = composition.ID
	r.Composition = cloneComposition(composition)
	r.Spec = cloneSpec(resolved)
	r.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)
	s.auditLocked(*r, "draft.updated", principal, "", "normal")
	if err := s.save(); err != nil {
		return portalapi.Revision{}, err
	}
	return cloneRevision(*r), nil
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
	if err := require(principal, "portal.compose"); err != nil {
		return portalapi.Revision{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.revisionIndex(principal.TenantID, id)
	if err != nil {
		return portalapi.Revision{}, err
	}
	r := &s.state.Revisions[i]
	if r.Status != portalapi.StatusDraft {
		return portalapi.Revision{}, ErrInvalidState
	}
	resolved, err := s.resolveCurrent(r.Composition, principal.TenantID, r.ID)
	if err != nil {
		return portalapi.Revision{}, err
	}
	if err := s.validateCatalog(ctx, principal.TenantID, resolved); err != nil {
		return portalapi.Revision{}, fmt.Errorf("%w: %v", ErrCatalogRejected, err)
	}
	r.Status = portalapi.StatusPendingApproval
	r.Spec = cloneSpec(resolved)
	r.SubmittedBy = principal.ID
	r.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)
	s.auditLocked(*r, "draft.submitted", principal, "", "normal")
	if err := s.save(); err != nil {
		return portalapi.Revision{}, err
	}
	return cloneRevision(*r), nil
}

func (s *Service) Approve(_ context.Context, principal portalapi.Principal, id uint64) (portalapi.Revision, error) {
	if err := require(principal, "portal.approve"); err != nil {
		return portalapi.Revision{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.revisionIndex(principal.TenantID, id)
	if err != nil {
		return portalapi.Revision{}, err
	}
	r := &s.state.Revisions[i]
	if r.Status != portalapi.StatusPendingApproval {
		return portalapi.Revision{}, ErrInvalidState
	}
	if r.SubmittedBy == principal.ID {
		return portalapi.Revision{}, ErrSelfApproval
	}
	r.Status = portalapi.StatusApproved
	r.ApprovedBy = principal.ID
	r.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)
	s.auditLocked(*r, "draft.approved", principal, "", "normal")
	if err := s.save(); err != nil {
		return portalapi.Revision{}, err
	}
	return cloneRevision(*r), nil
}

func (s *Service) Publish(ctx context.Context, principal portalapi.Principal, id uint64, request portalapi.PublishRequest) (portalapi.Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.revisionIndex(principal.TenantID, id)
	if err != nil {
		return portalapi.Revision{}, err
	}
	r := &s.state.Revisions[i]
	breakGlass := principal.System
	if !breakGlass {
		if err := require(principal, "portal.publish"); err != nil {
			return portalapi.Revision{}, err
		}
		if r.Status != portalapi.StatusApproved {
			return portalapi.Revision{}, ErrInvalidState
		}
	} else if strings.TrimSpace(request.BreakGlassReason) == "" {
		return portalapi.Revision{}, errors.New("system break-glass 发布必须说明原因")
	}
	resolved, err := s.resolveCurrent(r.Composition, principal.TenantID, r.ID)
	if err != nil {
		return portalapi.Revision{}, err
	}
	if s.routeConflictLocked(*r) {
		return portalapi.Revision{}, ErrRouteConflict
	}
	if err := s.materializeCatalog(ctx, principal.TenantID, resolved); err != nil {
		return portalapi.Revision{}, fmt.Errorf("%w: %v", ErrCatalogRejected, err)
	}
	r.Status = portalapi.StatusPublished
	r.Spec = cloneSpec(resolved)
	s.activateLocked(r)
	r.PublishedBy = principal.ID
	r.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)
	priority := "normal"
	action := "revision.published"
	if breakGlass {
		priority = "high"
		action = "revision.break_glass_published"
	}
	s.auditLocked(*r, action, principal, request.BreakGlassReason, priority)
	if err := s.save(); err != nil {
		return portalapi.Revision{}, err
	}
	return cloneRevision(*r), nil
}

func (s *Service) Rollback(ctx context.Context, principal portalapi.Principal, id uint64, request portalapi.PublishRequest) (portalapi.Revision, error) {
	if !principal.System {
		if err := require(principal, "portal.publish"); err != nil {
			return portalapi.Revision{}, err
		}
	} else if strings.TrimSpace(request.BreakGlassReason) == "" {
		return portalapi.Revision{}, errors.New("system break-glass 回滚必须说明原因")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.revisionIndex(principal.TenantID, id)
	if err != nil {
		return portalapi.Revision{}, err
	}
	source := s.state.Revisions[i]
	// A rollback selects an inactive, previously published revision. Allowing
	// the active revision would create a duplicate of the currently live state
	// rather than perform a rollback.
	if source.Status != portalapi.StatusPublished || source.Active {
		return portalapi.Revision{}, ErrInvalidState
	}
	resolved, err := s.resolveCurrent(source.Composition, principal.TenantID, s.state.NextRevision+1)
	if err != nil {
		return portalapi.Revision{}, err
	}
	s.state.NextRevision++
	now := s.now().UTC().Format(time.RFC3339Nano)
	r := portalapi.Revision{ID: s.state.NextRevision, TenantID: principal.TenantID, PortalID: source.PortalID, Status: portalapi.StatusPublished, Composition: cloneComposition(source.Composition), Spec: cloneSpec(resolved), PublishedBy: principal.ID, CreatedAt: now, UpdatedAt: now}
	if s.routeConflictLocked(r) {
		return portalapi.Revision{}, ErrRouteConflict
	}
	if err := s.materializeCatalog(ctx, principal.TenantID, resolved); err != nil {
		return portalapi.Revision{}, fmt.Errorf("%w: %v", ErrCatalogRejected, err)
	}
	s.state.Revisions = append(s.state.Revisions, r)
	s.activateLocked(&s.state.Revisions[len(s.state.Revisions)-1])
	priority := "normal"
	if principal.System {
		priority = "high"
	}
	s.auditLocked(r, "revision.rolled_back", principal, request.BreakGlassReason, priority)
	if err := s.save(); err != nil {
		return portalapi.Revision{}, err
	}
	return cloneRevision(s.state.Revisions[len(s.state.Revisions)-1]), nil
}

func (s *Service) Audit(_ context.Context, principal portalapi.Principal, id uint64) ([]portalapi.AuditEvent, error) {
	if principal.TenantID == "" || principal.ID == "" {
		return nil, ErrForbidden
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.revisionIndex(principal.TenantID, id); err != nil {
		return nil, err
	}
	out := make([]portalapi.AuditEvent, 0)
	for _, e := range s.state.Audit {
		if e.TenantID == principal.TenantID && e.RevisionID == id {
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
func (s *Service) activateLocked(candidate *portalapi.Revision) {
	for i := range s.state.Revisions {
		r := &s.state.Revisions[i]
		if r.TenantID == candidate.TenantID && r.PortalID == candidate.PortalID && r.ID != candidate.ID {
			r.Active = false
		}
	}
	candidate.Active = true
}

func (s *Service) routeConflictLocked(candidate portalapi.Revision) bool {
	for _, r := range s.state.Revisions {
		if r.TenantID != candidate.TenantID || r.ID == candidate.ID || !r.Active || r.Status != portalapi.StatusPublished || r.PortalID == candidate.PortalID {
			continue
		}
		if r.Spec.Route == candidate.Spec.Route {
			return true
		}
		for _, d := range r.Spec.Domains {
			for _, c := range candidate.Spec.Domains {
				if d == c {
					return true
				}
			}
		}
	}
	return false
}
func require(p portalapi.Principal, role string) error {
	if p.ID == "" || p.TenantID == "" {
		return ErrForbidden
	}
	if p.System {
		return nil
	}
	for _, r := range p.Roles {
		if r == role {
			return nil
		}
	}
	return ErrForbidden
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
	out.Layout.Config = cloneMap(in.Layout.Config)
	out.Management.Services = cloneManagedServices(in.Management.Services)
	out.Resolution.PluginOrigins = cloneStringMap(in.Resolution.PluginOrigins)
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
