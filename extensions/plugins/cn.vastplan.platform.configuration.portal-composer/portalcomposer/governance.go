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
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

func (s *Service) seedPublishedCatalogLocked(catalog frontendcompositionv1.PortalPlatformCatalog, tenant string) (bool, error) {
	beforeGovernance := s.state.NextGovernance
	profileRevisionByDigest := map[string]uint64{}
	for _, existing := range s.state.Profiles {
		if existing.Status == portalapi.StatusPublished {
			profileRevisionByDigest[existing.Profile.Digest()] = existing.ID
		}
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	for _, profile := range catalog.Profiles {
		digest := profile.Digest()
		if _, exists := profileRevisionByDigest[digest]; exists {
			continue
		}
		s.state.NextGovernance++
		revision := portalapi.PlatformProfileRevision{ID: s.state.NextGovernance, TenantID: "*", Status: portalapi.StatusPublished, Profile: profile, PublishedBy: "system", CreatedAt: now, UpdatedAt: now}
		s.state.Profiles = append(s.state.Profiles, revision)
		profileRevisionByDigest[digest] = revision.ID
	}
	for _, binding := range catalog.Bindings {
		if binding.TenantID != tenant {
			continue
		}
		profileRevisionID := profileRevisionByDigest[binding.PlatformProfile.Digest]
		if profileRevisionID == 0 {
			return false, fmt.Errorf("Portal Binding %s/%s 未找到已发布 Profile revision", binding.TenantID, binding.PortalID)
		}
		duplicate := false
		for _, existing := range s.state.Bindings {
			if existing.Status == portalapi.StatusPublished && existing.TenantID == binding.TenantID && existing.PortalID == binding.PortalID && existing.ProfileRevisionID == profileRevisionID && bindingDigest(existing.Binding) == bindingDigest(binding) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		s.state.NextGovernance++
		s.state.Bindings = append(s.state.Bindings, portalapi.BindingRevision{ID: s.state.NextGovernance, TenantID: binding.TenantID, PortalID: binding.PortalID, ProfileRevisionID: profileRevisionID, Status: portalapi.StatusPublished, Binding: binding, PublishedBy: "system", CreatedAt: now, UpdatedAt: now})
	}
	return s.state.NextGovernance != beforeGovernance, nil
}

func (s *Service) Governance(ctx context.Context, principal portalapi.Principal) (portalapi.GovernanceSnapshot, error) {
	_ = s.reconcilePortalReferences(ctx, principal)
	applications, err := s.List(ctx, principal)
	if err != nil {
		return portalapi.GovernanceSnapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	profiles := make([]portalapi.PlatformProfileRevision, 0)
	for _, revision := range s.state.Profiles {
		if revision.TenantID == "*" || revision.TenantID == principal.TenantID {
			profiles = append(profiles, cloneJSON(revision))
		}
	}
	bindings := make([]portalapi.BindingRevision, 0)
	for _, revision := range s.state.Bindings {
		if revision.TenantID == principal.TenantID {
			bindings = append(bindings, cloneJSON(revision))
		}
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ID > profiles[j].ID })
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].ID > bindings[j].ID })
	return portalapi.GovernanceSnapshot{Profiles: profiles, Applications: applications, Bindings: bindings, Activations: s.projectActivationsLocked(principal.TenantID)}, nil
}

func (s *Service) CreateProfileDraft(_ context.Context, principal portalapi.Principal, profile frontendcompositionv1.PlatformProfile) (portalapi.PlatformProfileRevision, error) {
	if err := require(principal, "portal.compose"); err != nil {
		return portalapi.PlatformProfileRevision{}, err
	}
	profile, err := validateProfile(profile)
	if err != nil {
		return portalapi.PlatformProfileRevision{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.NextGovernance++
	now := s.now().UTC().Format(time.RFC3339Nano)
	revision := portalapi.PlatformProfileRevision{ID: s.state.NextGovernance, TenantID: principal.TenantID, Status: portalapi.StatusDraft, Profile: profile, CreatedAt: now, UpdatedAt: now}
	s.state.Profiles = append(s.state.Profiles, revision)
	s.auditResourceLocked(principal.TenantID, profile.ID, revision.ID, "profile.draft.created", principal)
	return cloneJSON(revision), s.save()
}

func (s *Service) UpdateProfileDraft(_ context.Context, principal portalapi.Principal, id uint64, profile frontendcompositionv1.PlatformProfile) (portalapi.PlatformProfileRevision, error) {
	if err := require(principal, "portal.compose"); err != nil {
		return portalapi.PlatformProfileRevision{}, err
	}
	profile, err := validateProfile(profile)
	if err != nil {
		return portalapi.PlatformProfileRevision{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.profileIndexLocked(principal.TenantID, id)
	if err != nil {
		return portalapi.PlatformProfileRevision{}, err
	}
	revision := &s.state.Profiles[index]
	if revision.TenantID != principal.TenantID {
		return portalapi.PlatformProfileRevision{}, ErrForbidden
	}
	if revision.Status != portalapi.StatusDraft {
		return portalapi.PlatformProfileRevision{}, ErrInvalidState
	}
	revision.Profile, revision.UpdatedAt = profile, s.now().UTC().Format(time.RFC3339Nano)
	s.auditResourceLocked(principal.TenantID, profile.ID, id, "profile.draft.updated", principal)
	return cloneJSON(*revision), s.save()
}

func (s *Service) TransitionProfile(_ context.Context, principal portalapi.Principal, id uint64, action string) (portalapi.PlatformProfileRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.profileIndexLocked(principal.TenantID, id)
	if err != nil {
		return portalapi.PlatformProfileRevision{}, err
	}
	revision := &s.state.Profiles[index]
	if revision.TenantID != principal.TenantID {
		return portalapi.PlatformProfileRevision{}, ErrForbidden
	}
	status, err := transitionStatus(principal, revision.Status, revision.SubmittedBy, action)
	if err != nil {
		return portalapi.PlatformProfileRevision{}, err
	}
	revision.Status, revision.UpdatedAt = status, s.now().UTC().Format(time.RFC3339Nano)
	applyActors(&revision.SubmittedBy, &revision.ApprovedBy, &revision.PublishedBy, principal.ID, action)
	s.auditResourceLocked(principal.TenantID, revision.Profile.ID, id, "profile."+action, principal)
	return cloneJSON(*revision), s.save()
}

func (s *Service) CreateBindingDraft(_ context.Context, principal portalapi.Principal, request portalapi.BindingDraftRequest) (portalapi.BindingRevision, error) {
	if err := require(principal, "portal.compose"); err != nil {
		return portalapi.BindingRevision{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, err := s.validateBindingLocked(principal.TenantID, request)
	if err != nil {
		return portalapi.BindingRevision{}, err
	}
	s.state.NextGovernance++
	now := s.now().UTC().Format(time.RFC3339Nano)
	revision := portalapi.BindingRevision{ID: s.state.NextGovernance, TenantID: principal.TenantID, PortalID: binding.PortalID, ProfileRevisionID: request.ProfileRevisionID, Status: portalapi.StatusDraft, Binding: binding, CreatedAt: now, UpdatedAt: now}
	s.state.Bindings = append(s.state.Bindings, revision)
	s.auditResourceLocked(principal.TenantID, binding.PortalID, revision.ID, "binding.draft.created", principal)
	return cloneJSON(revision), s.save()
}

func (s *Service) UpdateBindingDraft(_ context.Context, principal portalapi.Principal, id uint64, request portalapi.BindingDraftRequest) (portalapi.BindingRevision, error) {
	if err := require(principal, "portal.compose"); err != nil {
		return portalapi.BindingRevision{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.bindingIndexLocked(principal.TenantID, id)
	if err != nil {
		return portalapi.BindingRevision{}, err
	}
	if s.state.Bindings[index].Status != portalapi.StatusDraft {
		return portalapi.BindingRevision{}, ErrInvalidState
	}
	binding, err := s.validateBindingLocked(principal.TenantID, request)
	if err != nil {
		return portalapi.BindingRevision{}, err
	}
	revision := &s.state.Bindings[index]
	revision.PortalID, revision.ProfileRevisionID, revision.Binding, revision.UpdatedAt = binding.PortalID, request.ProfileRevisionID, binding, s.now().UTC().Format(time.RFC3339Nano)
	s.auditResourceLocked(principal.TenantID, binding.PortalID, id, "binding.draft.updated", principal)
	return cloneJSON(*revision), s.save()
}

func (s *Service) TransitionBinding(_ context.Context, principal portalapi.Principal, id uint64, action string) (portalapi.BindingRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.bindingIndexLocked(principal.TenantID, id)
	if err != nil {
		return portalapi.BindingRevision{}, err
	}
	revision := &s.state.Bindings[index]
	status, err := transitionStatus(principal, revision.Status, revision.SubmittedBy, action)
	if err != nil {
		return portalapi.BindingRevision{}, err
	}
	revision.Status, revision.UpdatedAt = status, s.now().UTC().Format(time.RFC3339Nano)
	applyActors(&revision.SubmittedBy, &revision.ApprovedBy, &revision.PublishedBy, principal.ID, action)
	s.auditResourceLocked(principal.TenantID, revision.PortalID, id, "binding."+action, principal)
	return cloneJSON(*revision), s.save()
}

func (s *Service) Activate(ctx context.Context, principal portalapi.Principal, request portalapi.ActivationRequest) (portalapi.PortalActivation, error) {
	if err := require(principal, "portal.publish"); err != nil {
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

func (s *Service) validateBindingLocked(tenantID string, request portalapi.BindingDraftRequest) (frontendcompositionv1.PortalBinding, error) {
	profileIndex, err := s.profileIndexLocked(tenantID, request.ProfileRevisionID)
	if err != nil {
		return frontendcompositionv1.PortalBinding{}, err
	}
	profile := s.state.Profiles[profileIndex]
	if profile.Status != portalapi.StatusPublished {
		return frontendcompositionv1.PortalBinding{}, ErrInvalidState
	}
	binding := request.Binding
	if binding.TenantID != tenantID || strings.TrimSpace(binding.PortalID) == "" {
		return frontendcompositionv1.PortalBinding{}, errors.New("Binding tenantId/portalId 无效")
	}
	binding.PlatformProfile = compositioncommonv1.Ref{ID: profile.Profile.ID, Revision: profile.Profile.Revision, Digest: profile.Profile.Digest()}
	catalog := activationCatalog(profile.Profile, binding)
	validated, err := frontendcompositionv1.ValidatePortalPlatformCatalog(catalog)
	if err != nil {
		return frontendcompositionv1.PortalBinding{}, err
	}
	return validated.Bindings[0], nil
}

func validateProfile(profile frontendcompositionv1.PlatformProfile) (frontendcompositionv1.PlatformProfile, error) {
	raw, err := json.Marshal(profile)
	if err != nil {
		return frontendcompositionv1.PlatformProfile{}, err
	}
	return frontendcompositionv1.ParsePlatformProfile(raw)
}

func transitionStatus(principal portalapi.Principal, current portalapi.Status, submittedBy, action string) (portalapi.Status, error) {
	role, expected, next := "portal.compose", portalapi.StatusDraft, portalapi.StatusPendingApproval
	switch action {
	case "submit":
	case "approve":
		role, expected, next = "portal.approve", portalapi.StatusPendingApproval, portalapi.StatusApproved
		if principal.ID == submittedBy {
			return "", ErrSelfApproval
		}
	case "publish":
		role, expected, next = "portal.publish", portalapi.StatusApproved, portalapi.StatusPublished
	default:
		return "", fmt.Errorf("未知资源状态动作 %q", action)
	}
	if err := require(principal, role); err != nil {
		return "", err
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

func bindingDigest(binding frontendcompositionv1.PortalBinding) string {
	return compositioncommonv1.Digest(binding)
}

func cloneJSON[T any](value T) T {
	raw, _ := json.Marshal(value)
	var out T
	_ = json.Unmarshal(raw, &out)
	return out
}
