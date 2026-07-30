package portalcomposer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func (s *Service) Activate(ctx context.Context, principal portalapi.Principal, request portalapi.ActivationRequest) (portalapi.PortalActivation, error) {
	if err := requireTrustedPrincipal(principal); err != nil {
		return portalapi.PortalActivation{}, err
	}
	s.mu.Lock()
	currentID := s.currentActivationIDLocked(principal.TenantID, request.PortalID)
	if currentID != request.ExpectedCurrentID {
		s.mu.Unlock()
		return portalapi.PortalActivation{}, fmt.Errorf("%w: 当前 Activation 已从 %d 变为 %d", ErrInvalidState, request.ExpectedCurrentID, currentID)
	}
	s.state.NextActivation++
	now := s.now().UTC().Format(time.RFC3339Nano)
	activation := portalapi.PortalActivation{ID: s.state.NextActivation, TenantID: principal.TenantID, PortalID: request.PortalID, Status: portalapi.ActivationPreparing, ApplicationRevisionID: request.ApplicationRevisionID, ProfileRevisionID: request.ProfileRevisionID, BindingRevisionID: request.BindingRevisionID, PreviousActivationID: currentID, ActorID: principal.ID, Reason: request.Reason, CreatedAt: now}
	var previousReferences []pluginv1.ArtifactReference
	for _, candidate := range s.state.Activations {
		if candidate.TenantID == principal.TenantID && candidate.ID == currentID {
			previousReferences = append([]pluginv1.ArtifactReference(nil), candidate.ArtifactReferences...)
			break
		}
	}
	application, profile, binding, err := s.activationInputsLocked(principal.TenantID, request)
	s.mu.Unlock()
	if err != nil {
		return s.persistFailedActivation(activation, "validate-inputs", err)
	}
	phase := func(name string) portalapi.ActivationPhase {
		return portalapi.ActivationPhase{Name: name, Status: "Succeeded", StartedAt: now, EndedAt: s.now().UTC().Format(time.RFC3339Nano)}
	}
	activation.Phases = append(activation.Phases, phase("validate-inputs"))
	catalog := activationCatalog(profile.Profile, binding.Binding)
	spec, err := resolve(catalog, application.Composition, principal.TenantID, activation.ID)
	if err != nil {
		return s.persistFailedActivation(activation, "generate-snapshot", err)
	}
	activation.Spec = cloneSpec(spec)
	activation.Phases = append(activation.Phases, phase("generate-snapshot"))
	references, err := s.materializeCatalog(ctx, principal.TenantID, spec)
	if err != nil {
		return s.persistFailedActivation(activation, "edge-readiness", fmt.Errorf("%w: %v", ErrCatalogRejected, err))
	}
	activation.ArtifactReferences = withPortalPurpose(references, "active")
	activation.Phases = append(activation.Phases, phase("edge-readiness"))
	if err := s.protectPortalTransition(ctx, activation.ID, activation.PortalID, previousReferences, references); err != nil {
		_ = s.restorePortalActiveReferences(ctx, activation.ID, activation.PortalID, previousReferences)
		return s.persistFailedActivation(activation, "reference-protection", err)
	}
	activation.Phases = append(activation.Phases, phase("reference-protection"))

	// Expensive resolution and materialization intentionally run without the
	// governance mutex. Re-enter the critical section and revalidate the exact
	// tuple plus current Activation before the single live-state commit.
	s.mu.Lock()
	if current := s.currentActivationIDLocked(principal.TenantID, request.PortalID); current != request.ExpectedCurrentID {
		value, err := s.persistFailedActivationLocked(activation, "cas-activate", fmt.Errorf("%w: 当前 Activation 已从 %d 变为 %d", ErrInvalidState, request.ExpectedCurrentID, current))
		s.mu.Unlock()
		_ = s.restorePortalActiveReferences(ctx, activation.ID, activation.PortalID, previousReferences)
		return value, err
	}
	if _, _, _, err := s.activationInputsLocked(principal.TenantID, request); err != nil {
		value, persistErr := s.persistFailedActivationLocked(activation, "cas-activate", err)
		s.mu.Unlock()
		_ = s.restorePortalActiveReferences(ctx, activation.ID, activation.PortalID, previousReferences)
		return value, persistErr
	}
	if s.activationRouteConflictLocked(principal.TenantID, request.PortalID, spec) {
		value, err := s.persistFailedActivationLocked(activation, "cas-activate", ErrRouteConflict)
		s.mu.Unlock()
		_ = s.restorePortalActiveReferences(ctx, activation.ID, activation.PortalID, previousReferences)
		return value, err
	}
	activation.Phases = append(activation.Phases, phase("cas-activate"))
	activation.Status = portalapi.ActivationCurrent
	activation.ReferencePending = true
	s.state.Activations = append(s.state.Activations, activation)
	s.auditResourceLocked(principal.TenantID, request.PortalID, activation.ID, "activation.current", principal)
	if err := s.save(); err != nil {
		s.mu.Unlock()
		return portalapi.PortalActivation{}, err
	}
	s.mu.Unlock()

	if err := s.publishPortalReferences(ctx, activation, previousReferences); err != nil {
		return cloneJSON(activation), nil
	}
	s.mu.Lock()
	for i := range s.state.Activations {
		if s.state.Activations[i].TenantID == activation.TenantID && s.state.Activations[i].ID == activation.ID {
			s.state.Activations[i].ReferencePending = false
			activation.ReferencePending = false
			if err := s.save(); err != nil {
				s.state.Activations[i].ReferencePending = true
				activation.ReferencePending = true
			}
			break
		}
	}
	s.mu.Unlock()
	return cloneJSON(activation), nil
}

func (s *Service) persistFailedActivation(activation portalapi.PortalActivation, phaseName string, cause error) (portalapi.PortalActivation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistFailedActivationLocked(activation, phaseName, cause)
}

func (s *Service) persistFailedActivationLocked(activation portalapi.PortalActivation, phaseName string, cause error) (portalapi.PortalActivation, error) {
	now := s.now().UTC().Format(time.RFC3339Nano)
	activation.Status = portalapi.ActivationFailed
	activation.Phases = append(activation.Phases, portalapi.ActivationPhase{Name: phaseName, Status: "Failed", Message: cause.Error(), StartedAt: now, EndedAt: now})
	s.state.Activations = append(s.state.Activations, activation)
	s.auditResourceLocked(activation.TenantID, activation.PortalID, activation.ID, "activation.failed", portalapi.Principal{ID: activation.ActorID, TenantID: activation.TenantID})
	if err := s.save(); err != nil {
		return portalapi.PortalActivation{}, err
	}
	return cloneJSON(activation), nil
}

func (s *Service) RollbackActivation(ctx context.Context, principal portalapi.Principal, sourceID, expectedCurrentID uint64, reason string) (portalapi.PortalActivation, error) {
	if strings.TrimSpace(reason) == "" {
		return portalapi.PortalActivation{}, errors.New("Activation 回滚必须说明原因")
	}
	s.mu.Lock()
	var source portalapi.PortalActivation
	for _, candidate := range s.projectActivationsLocked(principal.TenantID) {
		if candidate.ID == sourceID {
			source = candidate
			break
		}
	}
	s.mu.Unlock()
	if source.ID == 0 || source.Status != portalapi.ActivationSuperseded {
		return portalapi.PortalActivation{}, ErrInvalidState
	}
	return s.Activate(ctx, principal, portalapi.ActivationRequest{PortalID: source.PortalID, ApplicationRevisionID: source.ApplicationRevisionID, ProfileRevisionID: source.ProfileRevisionID, BindingRevisionID: source.BindingRevisionID, ExpectedCurrentID: expectedCurrentID, Reason: reason})
}

func (s *Service) ListActivations(ctx context.Context, principal portalapi.Principal) ([]portalapi.PortalActivation, error) {
	if principal.ID == "" || principal.TenantID == "" {
		return nil, ErrForbidden
	}
	_ = s.reconcilePortalReferences(ctx, principal)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.projectActivationsLocked(principal.TenantID), nil
}

func (s *Service) projectActivationsLocked(tenantID string) []portalapi.PortalActivation {
	latest := map[string]uint64{}
	for _, activation := range s.state.Activations {
		if activation.TenantID == tenantID && activation.Status == portalapi.ActivationCurrent && activation.ID > latest[activation.PortalID] {
			latest[activation.PortalID] = activation.ID
		}
	}
	out := make([]portalapi.PortalActivation, 0)
	for _, activation := range s.state.Activations {
		if activation.TenantID != tenantID {
			continue
		}
		copy := cloneJSON(activation)
		if copy.Status == portalapi.ActivationCurrent && latest[copy.PortalID] != copy.ID {
			copy.Status = portalapi.ActivationSuperseded
		}
		out = append(out, copy)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

func (s *Service) activationInputsLocked(tenantID string, request portalapi.ActivationRequest) (portalapi.Revision, portalapi.PlatformProfileRevision, portalapi.BindingRevision, error) {
	applicationIndex, err := s.revisionIndex(tenantID, request.ApplicationRevisionID)
	if err != nil {
		return portalapi.Revision{}, portalapi.PlatformProfileRevision{}, portalapi.BindingRevision{}, err
	}
	application := s.state.Revisions[applicationIndex]
	profileIndex, err := s.profileIndexLocked(tenantID, request.ProfileRevisionID)
	if err != nil {
		return portalapi.Revision{}, portalapi.PlatformProfileRevision{}, portalapi.BindingRevision{}, err
	}
	profile := s.state.Profiles[profileIndex]
	bindingIndex, err := s.bindingIndexLocked(tenantID, request.BindingRevisionID)
	if err != nil {
		return portalapi.Revision{}, portalapi.PlatformProfileRevision{}, portalapi.BindingRevision{}, err
	}
	binding := s.state.Bindings[bindingIndex]
	if application.Status != portalapi.StatusPublished || profile.Status != portalapi.StatusPublished || binding.Status != portalapi.StatusPublished || application.PortalID != request.PortalID || binding.PortalID != request.PortalID || binding.ProfileRevisionID != profile.ID {
		return portalapi.Revision{}, portalapi.PlatformProfileRevision{}, portalapi.BindingRevision{}, ErrInvalidState
	}
	return application, profile, binding, nil
}

func activationCatalog(profile frontendcompositionv1.PlatformProfile, binding frontendcompositionv1.PortalBinding) frontendcompositionv1.PortalPlatformCatalog {
	binding.PlatformProfile = compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()}
	return frontendcompositionv1.PortalPlatformCatalog{Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "activation-catalog"}, Profiles: []frontendcompositionv1.PlatformProfile{profile}, Bindings: []frontendcompositionv1.PortalBinding{binding}}
}

func (s *Service) activationRouteConflictLocked(tenantID, portalID string, spec portalapi.PortalSpec) bool {
	for _, current := range s.projectActivationsLocked(tenantID) {
		if current.Status != portalapi.ActivationCurrent || current.PortalID == portalID {
			continue
		}
		if current.Spec.Route == spec.Route {
			return true
		}
		for _, domain := range current.Spec.Domains {
			for _, candidate := range spec.Domains {
				if domain == candidate {
					return true
				}
			}
		}
	}
	return false
}

func (s *Service) currentActivationIDLocked(tenantID, portalID string) uint64 {
	var current uint64
	for _, activation := range s.state.Activations {
		if activation.TenantID == tenantID && activation.PortalID == portalID && activation.Status == portalapi.ActivationCurrent && activation.ID > current {
			current = activation.ID
		}
	}
	return current
}

func (s *Service) profileIndexLocked(tenantID string, id uint64) (int, error) {
	for i, revision := range s.state.Profiles {
		if revision.ID == id && (revision.TenantID == tenantID || revision.TenantID == "*") {
			return i, nil
		}
	}
	return 0, ErrNotFound
}

func (s *Service) bindingIndexLocked(tenantID string, id uint64) (int, error) {
	for i, revision := range s.state.Bindings {
		if revision.ID == id && revision.TenantID == tenantID {
			return i, nil
		}
	}
	return 0, ErrNotFound
}

func transitionStatus(principal portalapi.Principal, current portalapi.Status, submittedBy, action string) (portalapi.Status, error) {
	if err := requireTrustedPrincipal(principal); err != nil {
		return "", err
	}
	expected, next := portalapi.StatusDraft, portalapi.StatusPendingApproval
	switch action {
	case "submit":
	case "approve":
		expected, next = portalapi.StatusPendingApproval, portalapi.StatusApproved
		if principal.ID == submittedBy {
			return "", ErrSelfApproval
		}
	case "publish":
		expected, next = portalapi.StatusApproved, portalapi.StatusPublished
	default:
		return "", fmt.Errorf("未知资源状态动作 %q", action)
	}
	if current != expected {
		return "", ErrInvalidState
	}
	return next, nil
}

func applyActors(submittedBy, approvedBy, publishedBy *string, actor, action string) {
	switch action {
	case "submit":
		*submittedBy = actor
	case "approve":
		*approvedBy = actor
	case "publish":
		*publishedBy = actor
	}
}

func (s *Service) auditResourceLocked(tenantID, resourceID string, revisionID uint64, action string, principal portalapi.Principal) {
	s.state.NextAudit++
	s.state.Audit = append(s.state.Audit, portalapi.AuditEvent{ID: s.state.NextAudit, TenantID: tenantID, PortalID: resourceID, RevisionID: revisionID, Action: action, ActorID: principal.ID, Priority: "normal", At: s.now().UTC().Format(time.RFC3339Nano)})
}

func cloneJSON[T any](value T) T {
	raw, _ := json.Marshal(value)
	var out T
	_ = json.Unmarshal(raw, &out)
	return out
}
