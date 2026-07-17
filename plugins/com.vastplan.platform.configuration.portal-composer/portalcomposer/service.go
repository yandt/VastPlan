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

	"cdsoft.com.cn/VastPlan/shared/go/portalapi"
)

const (
	PluginID      = "com.vastplan.platform.configuration.portal-composer"
	PluginVersion = "1.0.0"
	Capability    = "platform.portal-composer"
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
}

type state struct {
	NextRevision uint64                 `json:"nextRevision"`
	NextAudit    uint64                 `json:"nextAudit"`
	Revisions    []portalapi.Revision   `json:"revisions"`
	Audit        []portalapi.AuditEvent `json:"audit"`
}

type Service struct {
	mu        sync.Mutex
	state     state
	stateFile string
	catalog   Catalog
	now       func() time.Time
}

func New(stateFile string, catalog Catalog) (*Service, error) {
	if strings.TrimSpace(stateFile) == "" {
		return nil, errors.New("Portal Composer stateFile 不能为空")
	}
	if catalog == nil {
		return nil, errors.New("Portal Composer 必须注入受信任制品目录")
	}
	s := &Service{stateFile: stateFile, catalog: catalog, now: time.Now}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
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

func (s *Service) CreateDraft(ctx context.Context, principal portalapi.Principal, spec portalapi.PortalSpec) (portalapi.Revision, error) {
	if err := require(principal, "portal.compose"); err != nil {
		return portalapi.Revision{}, err
	}
	if err := validateSpec(spec); err != nil {
		return portalapi.Revision{}, err
	}
	if err := s.catalog.ValidatePortal(ctx, principal.TenantID, spec); err != nil {
		return portalapi.Revision{}, fmt.Errorf("%w: %v", ErrCatalogRejected, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC().Format(time.RFC3339Nano)
	s.state.NextRevision++
	r := portalapi.Revision{ID: s.state.NextRevision, TenantID: principal.TenantID, PortalID: spec.ID, Status: portalapi.StatusDraft, Spec: cloneSpec(spec), CreatedAt: now, UpdatedAt: now}
	s.state.Revisions = append(s.state.Revisions, r)
	s.auditLocked(r, "draft.created", principal, "", "normal")
	if err := s.save(); err != nil {
		return portalapi.Revision{}, err
	}
	return r, nil
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
	if err := s.catalog.ValidatePortal(ctx, principal.TenantID, r.Spec); err != nil {
		return portalapi.Revision{}, fmt.Errorf("%w: %v", ErrCatalogRejected, err)
	}
	r.Status = portalapi.StatusPendingApproval
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
	if err := s.catalog.ValidatePortal(ctx, principal.TenantID, r.Spec); err != nil {
		return portalapi.Revision{}, fmt.Errorf("%w: %v", ErrCatalogRejected, err)
	}
	if s.routeConflictLocked(*r) {
		return portalapi.Revision{}, ErrRouteConflict
	}
	r.Status = portalapi.StatusPublished
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
	if source.Status != portalapi.StatusPublished {
		return portalapi.Revision{}, ErrInvalidState
	}
	if err := s.catalog.ValidatePortal(ctx, principal.TenantID, source.Spec); err != nil {
		return portalapi.Revision{}, fmt.Errorf("%w: %v", ErrCatalogRejected, err)
	}
	s.state.NextRevision++
	now := s.now().UTC().Format(time.RFC3339Nano)
	r := portalapi.Revision{ID: s.state.NextRevision, TenantID: principal.TenantID, PortalID: source.PortalID, Status: portalapi.StatusPublished, Spec: cloneSpec(source.Spec), PublishedBy: principal.ID, CreatedAt: now, UpdatedAt: now}
	if s.routeConflictLocked(r) {
		return portalapi.Revision{}, ErrRouteConflict
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
	return r, nil
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
func validateSpec(s portalapi.PortalSpec) error {
	if strings.TrimSpace(s.ID) == "" || !strings.HasPrefix(s.Route, "/") {
		return errors.New("Portal 必须有 ID 和绝对 route")
	}
	if !strings.HasPrefix(s.DesignSystem.ID, "com.vastplan.foundation.frontend.design-system.") || s.DesignSystem.Version == "" || s.DesignSystem.UIContract == "" {
		return errors.New("Portal 必须选择精确版本的第一方设计系统和 UI 契约")
	}
	found := false
	seen := map[string]struct{}{}
	for _, p := range s.Plugins {
		if p.ID == "" || p.Version == "" {
			return errors.New("Portal 插件必须有精确 ID 和版本")
		}
		if _, ok := seen[p.ID]; ok {
			return errors.New("Portal 插件不能重复")
		}
		seen[p.ID] = struct{}{}
		if p.ID == s.DesignSystem.ID && p.Version == s.DesignSystem.Version {
			found = true
		}
	}
	if !found {
		return errors.New("Portal plugins 必须精确包含所选设计系统")
	}
	return nil
}
func cloneSpec(in portalapi.PortalSpec) portalapi.PortalSpec {
	out := in
	out.Domains = append([]string(nil), in.Domains...)
	out.Audience = append([]string(nil), in.Audience...)
	out.Plugins = append([]portalapi.PluginRef(nil), in.Plugins...)
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
func cloneRevision(in portalapi.Revision) portalapi.Revision { in.Spec = cloneSpec(in.Spec); return in }
