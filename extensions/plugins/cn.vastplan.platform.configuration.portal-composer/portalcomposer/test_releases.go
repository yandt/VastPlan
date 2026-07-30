package portalcomposer

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	semver "github.com/Masterminds/semver/v3"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginid"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

var testResourceID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

var (
	errTestBindingConflict = errors.New("Portal 测试目标绑定冲突")
	errTestReleaseConflict = errors.New("Portal 测试目标已有进行中发布")
	errTestArtifact        = errors.New("Portal 测试制品与目标绑定不匹配")
)

func (s *Service) ListTestTargetBindings(_ context.Context, principal portalapi.Principal) ([]portalapi.TestTargetBinding, error) {
	if principal.ID == "" || principal.TenantID == "" {
		return nil, ErrForbidden
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]portalapi.TestTargetBinding, 0, len(s.state.TestBindings))
	for _, binding := range s.state.TestBindings {
		if binding.TenantID == principal.TenantID && s.portalBelongsToTenantLocked(principal.TenantID, binding.PortalID) {
			out = append(out, cloneJSON(binding))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Service) PutTestTargetBinding(_ context.Context, principal portalapi.Principal, id string, request portalapi.PutTestTargetBindingRequest) (portalapi.TestTargetBinding, error) {
	if err := requireTrustedPrincipal(principal); err != nil {
		return portalapi.TestTargetBinding{}, err
	}
	id = strings.TrimSpace(id)
	publishers, err := normalizeTestPublishers(request.AllowedPublishers)
	if err != nil || !testResourceID.MatchString(id) || !validPortalTestScope(request.Scope) || !pluginid.IsFirstPartyID(request.PluginID) {
		return portalapi.TestTargetBinding{}, errTestArtifact
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	binding := portalapi.TestTargetBinding{
		ID: id, TenantID: principal.TenantID, Scope: request.Scope, PortalID: strings.TrimSpace(request.PortalID), PluginID: strings.TrimSpace(request.PluginID),
		AllowedPublishers: publishers, Enabled: request.Enabled, UpdatedAt: now,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.bindingMatchesCurrentApplicationLocked(principal.TenantID, binding) {
		return portalapi.TestTargetBinding{}, errTestArtifact
	}
	key := testBindingKey(principal.TenantID, id)
	for otherKey, other := range s.state.TestBindings {
		if otherKey != key && other.TenantID == principal.TenantID && other.PortalID == binding.PortalID && other.PluginID == binding.PluginID && other.Scope == binding.Scope {
			return portalapi.TestTargetBinding{}, errTestBindingConflict
		}
	}
	for _, release := range s.state.TestReleases {
		if release.TenantID == principal.TenantID && release.BindingID == id && !portalTestReleaseTerminal(release.Status) {
			return portalapi.TestTargetBinding{}, errTestReleaseConflict
		}
	}
	existing, exists := s.state.TestBindings[key]
	if exists {
		if request.IfVersion == nil || *request.IfVersion != existing.Version {
			return portalapi.TestTargetBinding{}, errTestBindingConflict
		}
		binding.Version, binding.CreatedAt = existing.Version+1, existing.CreatedAt
	} else {
		if request.IfVersion != nil && *request.IfVersion != 0 {
			return portalapi.TestTargetBinding{}, errTestBindingConflict
		}
		binding.Version, binding.CreatedAt = 1, now
	}
	s.state.TestBindings[key] = binding
	auditLength, nextAudit := len(s.state.Audit), s.state.NextAudit
	s.auditResourceLocked(principal.TenantID, binding.PortalID, uint64(binding.Version), "test_target_binding.saved", principal)
	if err := s.save(); err != nil {
		if exists {
			s.state.TestBindings[key] = existing
		} else {
			delete(s.state.TestBindings, key)
		}
		s.state.Audit, s.state.NextAudit = s.state.Audit[:auditLength], nextAudit
		return portalapi.TestTargetBinding{}, err
	}
	return cloneJSON(binding), nil
}

func (s *Service) ListTestReleases(_ context.Context, principal portalapi.Principal) ([]portalapi.TestRelease, error) {
	if principal.ID == "" || principal.TenantID == "" {
		return nil, ErrForbidden
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]portalapi.TestRelease, 0, len(s.state.TestReleases))
	for _, release := range s.state.TestReleases {
		binding, ok := s.state.TestBindings[testBindingKey(principal.TenantID, release.BindingID)]
		if release.TenantID == principal.TenantID && ok && s.portalBelongsToTenantLocked(principal.TenantID, binding.PortalID) {
			out = append(out, cloneJSON(release))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

func (s *Service) CreateTestRelease(ctx context.Context, principal portalapi.Principal, request portalapi.CreateTestReleaseRequest) (portalapi.TestRelease, error) {
	if err := requireTrustedPrincipal(principal); err != nil {
		return portalapi.TestRelease{}, err
	}
	if err := validatePortalTestArtifactRequest(request); err != nil {
		return portalapi.TestRelease{}, err
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	s.mu.Lock()
	binding, exists := s.state.TestBindings[testBindingKey(principal.TenantID, request.BindingID)]
	if !exists || !binding.Enabled || binding.PluginID != request.Receipt.Ref.PluginID || !s.bindingMatchesCurrentApplicationLocked(principal.TenantID, binding) {
		s.mu.Unlock()
		return portalapi.TestRelease{}, errTestArtifact
	}
	for _, current := range s.state.TestReleases {
		if current.BindingID == binding.ID && !portalTestReleaseTerminal(current.Status) {
			s.mu.Unlock()
			return portalapi.TestRelease{}, errTestReleaseConflict
		}
	}
	s.state.NextTestRelease++
	release := portalapi.TestRelease{
		ID: s.state.NextTestRelease, TenantID: principal.TenantID, BindingID: binding.ID, Receipt: request.Receipt,
		Status: portalapi.TestReleaseQueued, RequestedBy: principal.ID, CreatedAt: now, UpdatedAt: now,
	}
	s.state.TestReleases = append(s.state.TestReleases, release)
	if err := s.save(); err != nil {
		s.state.TestReleases = s.state.TestReleases[:len(s.state.TestReleases)-1]
		s.state.NextTestRelease--
		s.mu.Unlock()
		return portalapi.TestRelease{}, err
	}
	s.mu.Unlock()

	if err := s.publishPortalTestReleaseReference(ctx, release); err != nil {
		s.failPortalTestRelease(principal.TenantID, release.ID, "platform.portal_test_release.reference_protection_failed", err, false)
		return s.portalTestRelease(principal, release.ID)
	}
	s.executePortalTestRelease(ctx, principal, binding, request, release.ID)
	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cleanupCancel()
	_ = s.releasePortalTestReleaseReference(cleanupCtx, release)
	return s.portalTestRelease(principal, release.ID)
}

func (s *Service) executePortalTestRelease(ctx context.Context, principal portalapi.Principal, binding portalapi.TestTargetBinding, request portalapi.CreateTestReleaseRequest, releaseID uint64) {
	if err := s.transitionPortalTestRelease(principal.TenantID, releaseID, portalapi.TestReleaseResolving, nil); err != nil {
		return
	}
	if s.validateTestArtifact(ctx, principal.TenantID, request, binding.AllowedPublishers) != nil {
		s.failPortalTestRelease(principal.TenantID, releaseID, "platform.portal_test_release.artifact_rejected", errTestArtifact, false)
		return
	}
	if err := s.transitionPortalTestRelease(principal.TenantID, releaseID, portalapi.TestReleasePreparing, nil); err != nil {
		return
	}
	s.mu.Lock()
	currentBinding, exists := s.state.TestBindings[testBindingKey(principal.TenantID, binding.ID)]
	activation, application, profile, portalBinding, ok := s.currentPortalInputsLocked(principal.TenantID, binding.PortalID)
	if !exists || currentBinding.Version != binding.Version || !currentBinding.Enabled || !ok || !s.bindingMatchesCurrentApplicationLocked(principal.TenantID, binding) {
		s.mu.Unlock()
		s.failPortalTestRelease(principal.TenantID, releaseID, "platform.portal_test_release.target_changed", errTestArtifact, false)
		return
	}
	composition := cloneJSON(application.Composition)
	profileValue := cloneJSON(profile.Profile)
	configuration := portalapi.PortalConfiguration{Platform: profileValue, Application: composition, Services: cloneJSON(portalBinding.Binding.Services)}
	s.mu.Unlock()
	if binding.Scope == portalapi.TestTargetApplicationPlugin {
		if !replaceApplicationPlugin(&configuration.Application, binding.PluginID, request.Receipt.Ref) {
			s.failPortalTestRelease(principal.TenantID, releaseID, "platform.portal_test_release.target_changed", errTestArtifact, false)
			return
		}
	} else if !replaceProfilePlugin(&configuration.Platform, binding.PluginID, request.Receipt.Ref) {
		s.failPortalTestRelease(principal.TenantID, releaseID, "platform.portal_test_release.target_changed", errTestArtifact, false)
		return
	}
	if err := s.transitionPortalTestRelease(principal.TenantID, releaseID, portalapi.TestReleaseValidating, func(item *portalapi.TestRelease) {
		item.PreviousReleaseID = activation.ID
	}); err != nil {
		return
	}
	candidateVersion, err := s.createAuthorizedTestVersion(ctx, principal, binding.PortalID, configuration, releaseID, binding.ID)
	if err != nil {
		s.failPortalTestRelease(principal.TenantID, releaseID, "platform.portal_test_release.preview_failed", err, false)
		return
	}
	published, err := s.TransitionPortalVersion(ctx, principal, binding.PortalID, candidateVersion.ID, "publish")
	if err != nil {
		s.failPortalTestRelease(principal.TenantID, releaseID, "platform.portal_test_release.publish_failed", err, false)
		return
	}
	if err := s.transitionPortalTestRelease(principal.TenantID, releaseID, portalapi.TestReleaseActivating, func(item *portalapi.TestRelease) {
		item.CandidatePortalVersionID = published.ID
	}); err != nil {
		return
	}
	candidateRevision, err := s.legacyRevision(principal.TenantID, published.ID)
	if err != nil {
		s.failPortalTestRelease(principal.TenantID, releaseID, "platform.portal_test_release.publish_failed", err, false)
		return
	}
	candidate, err := s.Activate(ctx, principal, portalapi.ActivationRequest{
		PortalID: binding.PortalID, ApplicationRevisionID: published.ID, ProfileRevisionID: candidateRevision.ProfileRevisionID, BindingRevisionID: candidateRevision.BindingRevisionID,
		ExpectedCurrentID: activation.ID,
		Reason:            fmt.Sprintf("test-release:%d binding:%s", releaseID, binding.ID),
	})
	if err != nil || candidate.Status != portalapi.ActivationCurrent {
		cause := err
		if cause == nil {
			cause = fmt.Errorf("候选 Activation 状态为 %s", candidate.Status)
		}
		_ = s.transitionPortalTestRelease(principal.TenantID, releaseID, portalapi.TestReleaseRolledBack, func(item *portalapi.TestRelease) {
			item.CandidateReleaseID = candidate.ID
			item.ErrorCode, item.ErrorMessage = "platform.portal_test_release.activation_failed", cause.Error()
		})
		return
	}
	_ = s.transitionPortalTestRelease(principal.TenantID, releaseID, portalapi.TestReleaseReady, func(item *portalapi.TestRelease) {
		item.CandidateReleaseID = candidate.ID
	})
}

func (s *Service) RollbackTestRelease(ctx context.Context, principal portalapi.Principal, id uint64) (portalapi.TestRelease, error) {
	if err := requireTrustedPrincipal(principal); err != nil || id == 0 {
		return portalapi.TestRelease{}, ErrForbidden
	}
	release, err := s.portalTestRelease(principal, id)
	if err != nil || release.Status != portalapi.TestReleaseFailed || !release.RollbackRequired || release.PreviousReleaseID == 0 {
		return portalapi.TestRelease{}, ErrInvalidState
	}
	s.mu.Lock()
	binding := s.state.TestBindings[testBindingKey(principal.TenantID, release.BindingID)]
	currentID := s.currentActivationIDLocked(principal.TenantID, binding.PortalID)
	s.mu.Unlock()
	if currentID == release.PreviousReleaseID {
		_ = s.transitionPortalTestRelease(principal.TenantID, id, portalapi.TestReleaseRolledBack, func(item *portalapi.TestRelease) { item.RollbackRequired = false })
		return s.portalTestRelease(principal, id)
	}
	if release.CandidateReleaseID != 0 && currentID != release.CandidateReleaseID {
		return portalapi.TestRelease{}, ErrInvalidState
	}
	if err := s.transitionPortalTestRelease(principal.TenantID, id, portalapi.TestReleaseRollingBack, nil); err != nil {
		return portalapi.TestRelease{}, err
	}
	rolledBack, rollbackErr := s.RollbackActivation(ctx, principal, release.PreviousReleaseID, currentID, fmt.Sprintf("recover test-release:%d", id))
	if rollbackErr != nil || rolledBack.Status != portalapi.ActivationCurrent {
		s.failPortalTestRelease(principal.TenantID, id, "platform.portal_test_release.rollback_failed", coalescePortalError(rollbackErr, ErrInvalidState), true)
		return s.portalTestRelease(principal, id)
	}
	_ = s.transitionPortalTestRelease(principal.TenantID, id, portalapi.TestReleaseRolledBack, func(item *portalapi.TestRelease) {
		item.RollbackReleaseID, item.RollbackRequired = rolledBack.ID, false
	})
	return s.portalTestRelease(principal, id)
}

func (s *Service) createAuthorizedTestVersion(ctx context.Context, principal portalapi.Principal, portalID string, configuration portalapi.PortalConfiguration, releaseID uint64, bindingID string) (portalapi.PortalVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	number := uint64(1)
	for _, revision := range s.state.Revisions {
		if revision.TenantID == principal.TenantID && revision.PortalID == portalID && revision.Number >= number {
			number = revision.Number + 1
		}
	}
	configuration, spec, management, err := s.normalizePortalConfiguration(portalID, principal.TenantID, number, s.state.NextRevision+1, configuration)
	if err != nil {
		return portalapi.PortalVersion{}, err
	}
	if err := s.validateCatalog(ctx, principal.TenantID, spec); err != nil {
		return portalapi.PortalVersion{}, fmt.Errorf("%w: %v", ErrCatalogRejected, err)
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	s.state.NextRevision++
	versionID := s.state.NextRevision
	s.state.NextGovernance++
	profileID := s.state.NextGovernance
	s.state.NextGovernance++
	bindingRevisionID := s.state.NextGovernance
	submitter, approver := fmt.Sprintf("test-release:%d", releaseID), "test-target-binding:"+bindingID
	profile := portalapi.PlatformProfileRevision{ID: profileID, TenantID: principal.TenantID, Status: portalapi.StatusApproved, Profile: configuration.Platform, SubmittedBy: submitter, ApprovedBy: approver, CreatedAt: now, UpdatedAt: now}
	bindingRevision := portalapi.BindingRevision{ID: bindingRevisionID, TenantID: principal.TenantID, PortalID: portalID, ProfileRevisionID: profileID, Status: portalapi.StatusApproved, Binding: management, SubmittedBy: submitter, ApprovedBy: approver, CreatedAt: now, UpdatedAt: now}
	revision := portalapi.Revision{ID: versionID, Number: number, TenantID: principal.TenantID, PortalID: portalID, ProfileRevisionID: profileID, BindingRevisionID: bindingRevisionID, Status: portalapi.StatusApproved, Composition: configuration.Application, Spec: spec, SubmittedBy: submitter, ApprovedBy: approver, CreatedAt: now, UpdatedAt: now}
	s.state.Profiles = append(s.state.Profiles, profile)
	s.state.Bindings = append(s.state.Bindings, bindingRevision)
	s.state.Revisions = append(s.state.Revisions, revision)
	s.auditLocked(revision, "portal.version.test_target_authorized", portalapi.Principal{ID: approver, TenantID: principal.TenantID}, "", "normal")
	if err := s.save(); err != nil {
		return portalapi.PortalVersion{}, err
	}
	version, err := s.portalVersionLocked(principal.TenantID, revision)
	return version, err
}

func (s *Service) transitionPortalTestRelease(tenant string, id uint64, status portalapi.TestReleaseStatus, change func(*portalapi.TestRelease)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.state.TestReleases {
		item := &s.state.TestReleases[i]
		if item.ID != id {
			continue
		}
		old := cloneJSON(*item)
		item.Status, item.UpdatedAt = status, s.now().UTC().Format(time.RFC3339Nano)
		if change != nil {
			change(item)
		}
		if err := s.save(); err != nil {
			*item = old
			return err
		}
		return nil
	}
	return ErrNotFound
}

func (s *Service) failPortalTestRelease(tenant string, id uint64, code string, cause error, rollbackRequired bool) {
	_ = s.transitionPortalTestRelease(tenant, id, portalapi.TestReleaseFailed, func(item *portalapi.TestRelease) {
		item.ErrorCode, item.ErrorMessage, item.RollbackRequired = code, cause.Error(), rollbackRequired
	})
}

func (s *Service) portalTestRelease(principal portalapi.Principal, id uint64) (portalapi.TestRelease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, release := range s.state.TestReleases {
		binding, exists := s.state.TestBindings[testBindingKey(principal.TenantID, release.BindingID)]
		if release.ID == id && release.TenantID == principal.TenantID && exists && s.portalBelongsToTenantLocked(principal.TenantID, binding.PortalID) {
			return cloneJSON(release), nil
		}
	}
	return portalapi.TestRelease{}, ErrNotFound
}

func (s *Service) currentApplicationLocked(tenant, portalID string) (portalapi.PortalActivation, portalapi.Revision, bool) {
	activation, application, _, _, ok := s.currentPortalInputsLocked(tenant, portalID)
	return activation, application, ok
}

func (s *Service) currentPortalInputsLocked(tenant, portalID string) (portalapi.PortalActivation, portalapi.Revision, portalapi.PlatformProfileRevision, portalapi.BindingRevision, bool) {
	currentID := s.currentActivationIDLocked(tenant, portalID)
	for _, activation := range s.state.Activations {
		if activation.ID != currentID {
			continue
		}
		applicationIndex, appErr := s.revisionIndex(tenant, activation.ApplicationRevisionID)
		profileIndex, profileErr := s.profileIndexLocked(tenant, activation.ProfileRevisionID)
		bindingIndex, bindingErr := s.bindingIndexLocked(tenant, activation.BindingRevisionID)
		if appErr == nil && profileErr == nil && bindingErr == nil && s.state.Revisions[applicationIndex].Status == portalapi.StatusPublished && s.state.Profiles[profileIndex].Status == portalapi.StatusPublished && s.state.Bindings[bindingIndex].Status == portalapi.StatusPublished {
			return activation, s.state.Revisions[applicationIndex], s.state.Profiles[profileIndex], s.state.Bindings[bindingIndex], true
		}
	}
	return portalapi.PortalActivation{}, portalapi.Revision{}, portalapi.PlatformProfileRevision{}, portalapi.BindingRevision{}, false
}

func (s *Service) bindingMatchesCurrentApplicationLocked(tenant string, binding portalapi.TestTargetBinding) bool {
	activation, application, profile, _, ok := s.currentPortalInputsLocked(tenant, binding.PortalID)
	wantOrigin := compositioncommonv1.OriginApplication
	if binding.Scope == portalapi.TestTargetPlatformProfilePlugin {
		wantOrigin = compositioncommonv1.OriginPlatformProfile
	}
	if !ok || activation.Spec.Resolution.PluginOrigins[binding.PluginID] != wantOrigin {
		return false
	}
	refs := application.Composition.Plugins
	if binding.Scope == portalapi.TestTargetPlatformProfilePlugin {
		refs = profile.Profile.Plugins
	}
	for _, ref := range refs {
		if ref.ID == binding.PluginID {
			return true
		}
	}
	return false
}

func validPortalTestScope(scope portalapi.TestTargetScope) bool {
	return scope == portalapi.TestTargetApplicationPlugin || scope == portalapi.TestTargetPlatformProfilePlugin
}

func (s *Service) portalBelongsToTenantLocked(tenant, portalID string) bool {
	_, _, ok := s.currentApplicationLocked(tenant, portalID)
	return ok
}

func replaceApplicationPlugin(composition *frontendcompositionv1.ApplicationComposition, pluginID string, ref pluginv1.ArtifactRef) bool {
	for i := range composition.Plugins {
		if composition.Plugins[i].ID == pluginID {
			composition.Plugins[i] = frontendcompositionv1.PluginRef{ID: ref.PluginID, Version: ref.Version, Channel: ref.Channel}
			return true
		}
	}
	return false
}

func replaceProfilePlugin(profile *frontendcompositionv1.PlatformProfile, pluginID string, ref pluginv1.ArtifactRef) bool {
	replacement := frontendcompositionv1.PluginRef{ID: ref.PluginID, Version: ref.Version, Channel: ref.Channel}
	found := false
	for i := range profile.Plugins {
		if profile.Plugins[i].ID == pluginID {
			profile.Plugins[i], found = replacement, true
		}
	}
	if profile.RenderAdapter.ID == pluginID {
		profile.RenderAdapter.PluginRef, found = replacement, true
	}
	if profile.Shell.ID == pluginID {
		profile.Shell.PluginRef, found = replacement, true
	}
	if profile.Workbench.ID == pluginID {
		profile.Workbench.PluginRef, found = replacement, true
	}
	if found {
		profile.Revision++
	}
	return found
}

func validatePortalTestArtifactRequest(request portalapi.CreateTestReleaseRequest) error {
	if request.BindingID == "" || request.Receipt.Ref.PluginID == "" || request.Receipt.Ref.Channel != "testing" && request.Receipt.Ref.Channel != "workspace" || !regexp.MustCompile(`^[a-fA-F0-9]{64}$`).MatchString(request.Receipt.SHA256) {
		return errTestArtifact
	}
	if err := artifactrepositoryv1.ValidateReceiptShape(request.Receipt); err != nil {
		return errTestArtifact
	}
	version, err := semver.StrictNewVersion(request.Receipt.Ref.Version)
	if err != nil || version.Prerelease() == "" || !strings.HasPrefix(version.Prerelease(), "dev.") {
		return errTestArtifact
	}
	return nil
}

func normalizeTestPublishers(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !testResourceID.MatchString(value) {
			return nil, errTestArtifact
		}
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil, errTestArtifact
	}
	sort.Strings(out)
	return out, nil
}

func portalTestReleaseTerminal(status portalapi.TestReleaseStatus) bool {
	return status == portalapi.TestReleaseReady || status == portalapi.TestReleaseRolledBack || status == portalapi.TestReleaseFailed || status == portalapi.TestReleaseSuperseded
}

func (s *Service) recoverInterruptedTestReleases() bool {
	changed := false
	for i := range s.state.TestReleases {
		release := &s.state.TestReleases[i]
		if portalTestReleaseTerminal(release.Status) {
			continue
		}
		changed = true
		binding, ok := s.state.TestBindings[testBindingKey(release.TenantID, release.BindingID)]
		if !ok {
			release.Status, release.ErrorCode, release.ErrorMessage = portalapi.TestReleaseFailed, "platform.portal_test_release.interrupted", "重启时测试目标绑定不存在"
			release.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)
			continue
		}
		currentID := uint64(0)
		for _, activation := range s.state.Activations {
			if activation.TenantID == release.TenantID && activation.PortalID == binding.PortalID && activation.Status == portalapi.ActivationCurrent && activation.ID > currentID {
				currentID = activation.ID
				if release.CandidatePortalVersionID != 0 && activation.ApplicationRevisionID == release.CandidatePortalVersionID {
					release.CandidateReleaseID = activation.ID
				}
			}
		}
		release.Status = portalapi.TestReleaseFailed
		release.ErrorCode, release.ErrorMessage = "platform.portal_test_release.interrupted", "Portal Test Release 在非终态时重启，已 fail-closed"
		release.RollbackRequired = release.CandidateReleaseID != 0 && currentID == release.CandidateReleaseID && currentID != release.PreviousReleaseID
		release.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)
	}
	return changed
}

func testBindingKey(tenant, id string) string { return tenant + "\x00" + id }

func coalescePortalError(primary, fallback error) error {
	if primary != nil {
		return primary
	}
	return fallback
}
