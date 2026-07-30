package portalcomposer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

// CreatePortal creates the stable Portal lineage and its first draft version.
func (s *Service) CreatePortal(ctx context.Context, principal portalapi.Principal, request portalapi.PortalVersionRequest) (portalapi.Portal, error) {
	if err := requireTrustedPrincipal(principal); err != nil {
		return portalapi.Portal{}, err
	}
	portalID := strings.TrimSpace(request.PortalID)
	if portalID == "" {
		return portalapi.Portal{}, errors.New("portalId 不能为空")
	}
	configuration := request.Configuration
	if strings.TrimSpace(configuration.Platform.ID) == "" {
		configuration.Application.ID = portalID
		var err error
		configuration, err = s.configurationFromCatalog(configuration.Application, principal.TenantID)
		if err != nil {
			return portalapi.Portal{}, fmt.Errorf("Portal %s 没有可用的种子配置: %w", portalID, err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.portalExistsLocked(principal.TenantID, portalID) {
		return portalapi.Portal{}, fmt.Errorf("%w: Portal %s 已存在", ErrInvalidState, portalID)
	}
	if _, err := s.createPortalVersionLocked(ctx, principal, portalID, configuration, 1); err != nil {
		return portalapi.Portal{}, err
	}
	return s.portalLocked(principal.TenantID, portalID)
}

// CreatePortalVersion appends one draft to an existing Portal lineage. A
// lineage may have only one unpublished candidate at a time.
func (s *Service) CreatePortalVersion(ctx context.Context, principal portalapi.Principal, portalID string, configuration portalapi.PortalConfiguration) (portalapi.PortalVersion, error) {
	if err := requireTrustedPrincipal(principal); err != nil {
		return portalapi.PortalVersion{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.portalExistsLocked(principal.TenantID, portalID) {
		return portalapi.PortalVersion{}, ErrNotFound
	}
	number := uint64(1)
	for _, revision := range s.state.Revisions {
		if revision.TenantID != principal.TenantID || revision.PortalID != portalID || s.isTestVersionLocked(revision.ID) {
			continue
		}
		if revision.Status != portalapi.StatusPublished {
			return portalapi.PortalVersion{}, fmt.Errorf("%w: Portal 已有未发布版本 #%d", ErrInvalidState, revision.Number)
		}
		if revision.Number >= number {
			number = revision.Number + 1
		}
	}
	return s.createPortalVersionLocked(ctx, principal, portalID, configuration, number)
}

func (s *Service) createPortalVersionLocked(ctx context.Context, principal portalapi.Principal, portalID string, configuration portalapi.PortalConfiguration, number uint64) (portalapi.PortalVersion, error) {
	configuration, spec, binding, err := s.normalizePortalConfiguration(portalID, principal.TenantID, number, s.state.NextRevision+1, configuration)
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
	bindingID := s.state.NextGovernance

	profile := portalapi.PlatformProfileRevision{ID: profileID, TenantID: principal.TenantID, Status: portalapi.StatusDraft, Profile: configuration.Platform, CreatedAt: now, UpdatedAt: now}
	management := portalapi.BindingRevision{ID: bindingID, TenantID: principal.TenantID, PortalID: portalID, ProfileRevisionID: profileID, Status: portalapi.StatusDraft, Binding: binding, CreatedAt: now, UpdatedAt: now}
	revision := portalapi.Revision{ID: versionID, Number: number, TenantID: principal.TenantID, PortalID: portalID, ProfileRevisionID: profileID, BindingRevisionID: bindingID, Status: portalapi.StatusDraft, Composition: configuration.Application, Spec: spec, CreatedAt: now, UpdatedAt: now}
	s.state.Profiles = append(s.state.Profiles, profile)
	s.state.Bindings = append(s.state.Bindings, management)
	s.state.Revisions = append(s.state.Revisions, revision)
	s.auditLocked(revision, "portal.version.created", principal, "", "normal")
	if err := s.save(); err != nil {
		return portalapi.PortalVersion{}, err
	}
	return s.portalVersionLocked(principal.TenantID, revision)
}

func (s *Service) UpdatePortalVersion(ctx context.Context, principal portalapi.Principal, portalID string, id uint64, configuration portalapi.PortalConfiguration) (portalapi.PortalVersion, error) {
	if err := requireTrustedPrincipal(principal); err != nil {
		return portalapi.PortalVersion{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.revisionIndex(principal.TenantID, id)
	if err != nil || s.state.Revisions[index].PortalID != portalID {
		return portalapi.PortalVersion{}, ErrNotFound
	}
	revision := &s.state.Revisions[index]
	if revision.Status != portalapi.StatusDraft {
		return portalapi.PortalVersion{}, ErrInvalidState
	}
	configuration, spec, binding, err := s.normalizePortalConfiguration(portalID, principal.TenantID, revision.Number, revision.ID, configuration)
	if err != nil {
		return portalapi.PortalVersion{}, err
	}
	if err := s.validateCatalog(ctx, principal.TenantID, spec); err != nil {
		return portalapi.PortalVersion{}, fmt.Errorf("%w: %v", ErrCatalogRejected, err)
	}
	profileIndex, bindingIndex, err := s.versionPartsLocked(principal.TenantID, *revision)
	if err != nil {
		return portalapi.PortalVersion{}, err
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	revision.Composition, revision.Spec, revision.UpdatedAt = configuration.Application, spec, now
	s.state.Profiles[profileIndex].Profile, s.state.Profiles[profileIndex].UpdatedAt = configuration.Platform, now
	s.state.Bindings[bindingIndex].Binding, s.state.Bindings[bindingIndex].UpdatedAt = binding, now
	s.auditLocked(*revision, "portal.version.updated", principal, "", "normal")
	if err := s.save(); err != nil {
		return portalapi.PortalVersion{}, err
	}
	return s.portalVersionLocked(principal.TenantID, *revision)
}

func (s *Service) DeletePortalVersion(_ context.Context, principal portalapi.Principal, portalID string, id uint64) (portalapi.PortalVersion, error) {
	if err := requireTrustedPrincipal(principal); err != nil {
		return portalapi.PortalVersion{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.revisionIndex(principal.TenantID, id)
	if err != nil || s.state.Revisions[index].PortalID != portalID {
		return portalapi.PortalVersion{}, ErrNotFound
	}
	revision := s.state.Revisions[index]
	if revision.Status != portalapi.StatusDraft {
		return portalapi.PortalVersion{}, ErrInvalidState
	}
	value, err := s.portalVersionLocked(principal.TenantID, revision)
	if err != nil {
		return portalapi.PortalVersion{}, err
	}
	profileIndex, bindingIndex, err := s.versionPartsLocked(principal.TenantID, revision)
	if err != nil {
		return portalapi.PortalVersion{}, err
	}
	s.state.Revisions = append(s.state.Revisions[:index], s.state.Revisions[index+1:]...)
	s.state.Profiles = append(s.state.Profiles[:profileIndex], s.state.Profiles[profileIndex+1:]...)
	s.state.Bindings = append(s.state.Bindings[:bindingIndex], s.state.Bindings[bindingIndex+1:]...)
	s.auditLocked(revision, "portal.version.deleted", principal, "", "normal")
	return value, s.save()
}

func (s *Service) TransitionPortalVersion(ctx context.Context, principal portalapi.Principal, portalID string, id uint64, action string) (portalapi.PortalVersion, error) {
	return s.transitionPortalVersion(ctx, principal, portalID, id, action, false)
}

func (s *Service) transitionPortalVersion(ctx context.Context, principal portalapi.Principal, portalID string, id uint64, action string, allowTest bool) (portalapi.PortalVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.revisionIndex(principal.TenantID, id)
	if err != nil || s.state.Revisions[index].PortalID != portalID || s.isTestVersionLocked(id) != allowTest {
		return portalapi.PortalVersion{}, ErrNotFound
	}
	revision := &s.state.Revisions[index]
	profileIndex, bindingIndex, err := s.versionPartsLocked(principal.TenantID, *revision)
	if err != nil {
		return portalapi.PortalVersion{}, err
	}
	profile, binding := &s.state.Profiles[profileIndex], &s.state.Bindings[bindingIndex]
	if profile.Status != revision.Status || binding.Status != revision.Status {
		return portalapi.PortalVersion{}, errors.New("PortalVersion 内部状态不一致")
	}
	next, err := transitionStatus(principal, revision.Status, revision.SubmittedBy, action)
	if err != nil {
		return portalapi.PortalVersion{}, err
	}
	if action == "submit" || action == "publish" {
		configuration := portalapi.PortalConfiguration{Platform: profile.Profile, Application: revision.Composition, Services: binding.Binding.Services}
		_, spec, _, err := s.normalizePortalConfiguration(portalID, principal.TenantID, revision.Number, revision.ID, configuration)
		if err != nil {
			return portalapi.PortalVersion{}, err
		}
		if err := s.validateCatalog(ctx, principal.TenantID, spec); err != nil {
			return portalapi.PortalVersion{}, fmt.Errorf("%w: %v", ErrCatalogRejected, err)
		}
		revision.Spec = spec
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	revision.Status, profile.Status, binding.Status = next, next, next
	revision.UpdatedAt, profile.UpdatedAt, binding.UpdatedAt = now, now, now
	applyActors(&revision.SubmittedBy, &revision.ApprovedBy, &revision.PublishedBy, principal.ID, action)
	applyActors(&profile.SubmittedBy, &profile.ApprovedBy, &profile.PublishedBy, principal.ID, action)
	applyActors(&binding.SubmittedBy, &binding.ApprovedBy, &binding.PublishedBy, principal.ID, action)
	s.auditLocked(*revision, "portal.version."+action, principal, "", "normal")
	if err := s.save(); err != nil {
		return portalapi.PortalVersion{}, err
	}
	return s.portalVersionLocked(principal.TenantID, *revision)
}

func (s *Service) breakGlassPublishPortalVersion(ctx context.Context, principal portalapi.Principal, portalID string, id uint64, reason string) (portalapi.PortalVersion, error) {
	if !principal.System || strings.TrimSpace(reason) == "" {
		return portalapi.PortalVersion{}, errors.New("system break-glass 发布必须说明原因")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.revisionIndex(principal.TenantID, id)
	if err != nil || s.state.Revisions[index].PortalID != portalID || s.isTestVersionLocked(id) {
		return portalapi.PortalVersion{}, ErrNotFound
	}
	revision := &s.state.Revisions[index]
	profileIndex, bindingIndex, err := s.versionPartsLocked(principal.TenantID, *revision)
	if err != nil {
		return portalapi.PortalVersion{}, err
	}
	profile, binding := &s.state.Profiles[profileIndex], &s.state.Bindings[bindingIndex]
	configuration := portalapi.PortalConfiguration{Platform: profile.Profile, Application: revision.Composition, Services: binding.Binding.Services}
	_, spec, _, err := s.normalizePortalConfiguration(revision.PortalID, principal.TenantID, revision.Number, revision.ID, configuration)
	if err != nil {
		return portalapi.PortalVersion{}, err
	}
	if err := s.validateCatalog(ctx, principal.TenantID, spec); err != nil {
		return portalapi.PortalVersion{}, fmt.Errorf("%w: %v", ErrCatalogRejected, err)
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	revision.Status, profile.Status, binding.Status = portalapi.StatusPublished, portalapi.StatusPublished, portalapi.StatusPublished
	revision.Spec, revision.PublishedBy, revision.UpdatedAt = spec, principal.ID, now
	profile.PublishedBy, profile.UpdatedAt = principal.ID, now
	binding.PublishedBy, binding.UpdatedAt = principal.ID, now
	s.auditLocked(*revision, "portal.version.break_glass_published", principal, reason, "high")
	if err := s.save(); err != nil {
		return portalapi.PortalVersion{}, err
	}
	return s.portalVersionLocked(principal.TenantID, *revision)
}

func (s *Service) legacyRevision(tenantID string, id uint64) (portalapi.Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.revisionIndex(tenantID, id)
	if err != nil {
		return portalapi.Revision{}, err
	}
	return cloneRevision(s.state.Revisions[index]), nil
}

func (s *Service) ReleasePortalVersion(ctx context.Context, principal portalapi.Principal, portalID string, request portalapi.PortalReleaseRequest) (portalapi.PortalRelease, error) {
	s.mu.Lock()
	index, err := s.revisionIndex(principal.TenantID, request.PortalVersionID)
	if err != nil || s.state.Revisions[index].PortalID != portalID || s.isTestVersionLocked(request.PortalVersionID) {
		s.mu.Unlock()
		return portalapi.PortalRelease{}, ErrNotFound
	}
	revision := s.state.Revisions[index]
	if revision.Status != portalapi.StatusPublished || !s.isLatestPublishedVersionLocked(principal.TenantID, portalID, revision) || s.wasReleasedLocked(principal.TenantID, portalID, revision.ID) {
		s.mu.Unlock()
		return portalapi.PortalRelease{}, ErrInvalidState
	}
	s.mu.Unlock()
	activation, err := s.Activate(ctx, principal, portalapi.ActivationRequest{
		PortalID: portalID, ApplicationRevisionID: revision.ID, ProfileRevisionID: revision.ProfileRevisionID,
		BindingRevisionID: revision.BindingRevisionID, ExpectedCurrentID: request.ExpectedCurrentReleaseID, Reason: request.Reason,
	})
	return projectRelease(activation), err
}

func (s *Service) RollbackPortalRelease(ctx context.Context, principal portalapi.Principal, portalID string, sourceID, expectedCurrentID uint64, reason string) (portalapi.PortalRelease, error) {
	s.mu.Lock()
	valid := false
	for _, release := range s.state.Activations {
		if release.TenantID == principal.TenantID && release.PortalID == portalID && release.ID == sourceID && !s.isTestVersionLocked(release.ApplicationRevisionID) {
			valid = true
			break
		}
	}
	s.mu.Unlock()
	if !valid {
		return portalapi.PortalRelease{}, ErrNotFound
	}
	release, err := s.RollbackActivation(ctx, principal, sourceID, expectedCurrentID, reason)
	return projectRelease(release), err
}

func (s *Service) PortalGovernance(ctx context.Context, principal portalapi.Principal) (portalapi.PortalGovernanceSnapshot, error) {
	if principal.ID == "" || principal.TenantID == "" {
		return portalapi.PortalGovernanceSnapshot{}, ErrForbidden
	}
	_ = s.reconcilePortalReferences(ctx, principal)
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := map[string]struct{}{}
	for _, revision := range s.state.Revisions {
		if revision.TenantID == principal.TenantID && !s.isTestVersionLocked(revision.ID) {
			ids[revision.PortalID] = struct{}{}
		}
	}
	portals := make([]portalapi.Portal, 0, len(ids))
	for id := range ids {
		portal, err := s.portalLocked(principal.TenantID, id)
		if err != nil {
			return portalapi.PortalGovernanceSnapshot{}, err
		}
		portals = append(portals, portal)
	}
	sort.Slice(portals, func(i, j int) bool { return portals[i].ID < portals[j].ID })
	template := s.portalCreationTemplateLocked(principal.TenantID)
	return portalapi.PortalGovernanceSnapshot{Portals: portals, CreationTemplate: template}, nil
}

func (s *Service) ListPortalReleases(ctx context.Context, principal portalapi.Principal) ([]portalapi.PortalRelease, error) {
	if principal.ID == "" || principal.TenantID == "" {
		return nil, ErrForbidden
	}
	_ = s.reconcilePortalReferences(ctx, principal)
	s.mu.Lock()
	defer s.mu.Unlock()
	activations := s.projectActivationsLocked(principal.TenantID)
	releases := make([]portalapi.PortalRelease, 0, len(activations))
	for _, activation := range activations {
		releases = append(releases, projectRelease(activation))
	}
	return releases, nil
}

func (s *Service) portalLocked(tenantID, portalID string) (portalapi.Portal, error) {
	portal := portalapi.Portal{ID: portalID, TenantID: tenantID, Versions: []portalapi.PortalVersion{}, Releases: []portalapi.PortalRelease{}}
	for _, revision := range s.state.Revisions {
		if revision.TenantID != tenantID || revision.PortalID != portalID || s.isTestVersionLocked(revision.ID) {
			continue
		}
		version, err := s.portalVersionLocked(tenantID, revision)
		if err != nil {
			return portalapi.Portal{}, err
		}
		portal.Versions = append(portal.Versions, version)
		if portal.CreatedAt == "" || version.CreatedAt < portal.CreatedAt {
			portal.CreatedAt = version.CreatedAt
		}
		if version.UpdatedAt > portal.UpdatedAt {
			portal.UpdatedAt = version.UpdatedAt
		}
	}
	if len(portal.Versions) == 0 {
		return portalapi.Portal{}, ErrNotFound
	}
	sort.Slice(portal.Versions, func(i, j int) bool { return portal.Versions[i].Number > portal.Versions[j].Number })
	for _, activation := range s.projectPortalActivationsLocked(tenantID) {
		if activation.PortalID != portalID {
			continue
		}
		release := projectRelease(activation)
		portal.Releases = append(portal.Releases, release)
		if release.Status == portalapi.ActivationCurrent {
			portal.CurrentReleaseID = release.ID
		}
	}
	return portal, nil
}

func (s *Service) portalCreationTemplateLocked(tenantID string) *portalapi.PortalConfiguration {
	for _, binding := range s.platformCatalog.Bindings {
		if binding.TenantID != tenantID {
			continue
		}
		for _, profile := range s.platformCatalog.Profiles {
			if profile.ID != binding.PlatformProfile.ID || profile.Revision != binding.PlatformProfile.Revision || profile.Digest() != binding.PlatformProfile.Digest {
				continue
			}
			value := portalapi.PortalConfiguration{
				Platform: profile,
				Application: frontendcompositionv1.ApplicationComposition{
					Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "portal-template"},
					Target:   compositioncommonv1.Target{Kernel: compositioncommonv1.KernelFrontend},
					Route:    "/", Plugins: []frontendcompositionv1.PluginRef{}, Config: map[string]any{},
				},
				Services: cloneJSON(binding.Services),
			}
			return &value
		}
	}
	return nil
}

func (s *Service) isLatestPublishedVersionLocked(tenantID, portalID string, candidate portalapi.Revision) bool {
	for _, revision := range s.state.Revisions {
		if revision.TenantID == tenantID && revision.PortalID == portalID && !s.isTestVersionLocked(revision.ID) && revision.Status == portalapi.StatusPublished && revision.Number > candidate.Number {
			return false
		}
	}
	return true
}

func (s *Service) wasReleasedLocked(tenantID, portalID string, versionID uint64) bool {
	for _, release := range s.projectPortalActivationsLocked(tenantID) {
		if release.PortalID == portalID && release.ApplicationRevisionID == versionID && (release.Status == portalapi.ActivationCurrent || release.Status == portalapi.ActivationSuperseded) {
			return true
		}
	}
	return false
}

func (s *Service) portalVersionLocked(tenantID string, revision portalapi.Revision) (portalapi.PortalVersion, error) {
	profileIndex, bindingIndex, err := s.versionPartsLocked(tenantID, revision)
	if err != nil {
		return portalapi.PortalVersion{}, err
	}
	return portalapi.PortalVersion{
		ID: revision.ID, Number: revision.Number, TenantID: revision.TenantID, PortalID: revision.PortalID, Status: revision.Status,
		Configuration: portalapi.PortalConfiguration{Platform: cloneJSON(s.state.Profiles[profileIndex].Profile), Application: cloneComposition(revision.Composition), Services: cloneJSON(s.state.Bindings[bindingIndex].Binding.Services)},
		Resolved:      cloneSpec(revision.Spec), SubmittedBy: revision.SubmittedBy, ApprovedBy: revision.ApprovedBy, PublishedBy: revision.PublishedBy,
		CreatedAt: revision.CreatedAt, UpdatedAt: revision.UpdatedAt,
	}, nil
}

func (s *Service) versionPartsLocked(tenantID string, revision portalapi.Revision) (int, int, error) {
	profile, err := s.profileIndexLocked(tenantID, revision.ProfileRevisionID)
	if err != nil {
		return 0, 0, errors.New("PortalVersion 缺少内部平台配置")
	}
	binding, err := s.bindingIndexLocked(tenantID, revision.BindingRevisionID)
	if err != nil {
		return 0, 0, errors.New("PortalVersion 缺少内部服务绑定")
	}
	if s.state.Bindings[binding].ProfileRevisionID != revision.ProfileRevisionID || s.state.Bindings[binding].PortalID != revision.PortalID {
		return 0, 0, errors.New("PortalVersion 内部配置引用不一致")
	}
	return profile, binding, nil
}

func (s *Service) portalExistsLocked(tenantID, portalID string) bool {
	for _, revision := range s.state.Revisions {
		if revision.TenantID == tenantID && revision.PortalID == portalID && !s.isTestVersionLocked(revision.ID) {
			return true
		}
	}
	return false
}

func (s *Service) isTestVersionLocked(versionID uint64) bool {
	_, ok := s.state.TestVersionOwners[versionID]
	return ok
}

func (s *Service) normalizePortalConfiguration(portalID, tenantID string, number, resolvedRevision uint64, configuration portalapi.PortalConfiguration) (portalapi.PortalConfiguration, portalapi.PortalSpec, frontendcompositionv1.PortalBinding, error) {
	configuration.Platform.Document = compositioncommonv1.Document{Version: 1, Revision: number, ID: portalID + ".platform"}
	configuration.Platform.Target = compositioncommonv1.Target{Kernel: compositioncommonv1.KernelFrontend}
	profile, err := frontendcompositionv1.ValidatePlatformProfile(configuration.Platform)
	if err != nil {
		return portalapi.PortalConfiguration{}, portalapi.PortalSpec{}, frontendcompositionv1.PortalBinding{}, err
	}
	configuration.Application.Document = compositioncommonv1.Document{Version: 1, Revision: number, ID: portalID}
	configuration.Application.Target = compositioncommonv1.Target{Kernel: compositioncommonv1.KernelFrontend}
	application, err := frontendcompositionv1.ValidateApplicationComposition(configuration.Application)
	if err != nil {
		return portalapi.PortalConfiguration{}, portalapi.PortalSpec{}, frontendcompositionv1.PortalBinding{}, err
	}
	binding := frontendcompositionv1.PortalBinding{TenantID: tenantID, PortalID: portalID, PlatformProfile: compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()}, Services: cloneJSON(configuration.Services)}
	catalog := frontendcompositionv1.PortalPlatformCatalog{Document: compositioncommonv1.Document{Version: 1, Revision: number, ID: portalID + ".catalog"}, Profiles: []frontendcompositionv1.PlatformProfile{profile}, Bindings: []frontendcompositionv1.PortalBinding{binding}}
	spec, err := resolve(catalog, application, tenantID, resolvedRevision)
	if err != nil {
		return portalapi.PortalConfiguration{}, portalapi.PortalSpec{}, frontendcompositionv1.PortalBinding{}, err
	}
	configuration.Platform, configuration.Application, configuration.Services = profile, application, cloneJSON(binding.Services)
	return configuration, spec, binding, nil
}

func (s *Service) configurationFromCatalog(application frontendcompositionv1.ApplicationComposition, tenantID string) (portalapi.PortalConfiguration, error) {
	profile, binding, err := s.platformCatalog.Resolve(tenantID, application.ID)
	if err != nil {
		return portalapi.PortalConfiguration{}, err
	}
	return portalapi.PortalConfiguration{Platform: profile, Application: application, Services: cloneJSON(binding.Services)}, nil
}

func projectRelease(value portalapi.PortalActivation) portalapi.PortalRelease {
	return portalapi.PortalRelease{
		ID: value.ID, TenantID: value.TenantID, PortalID: value.PortalID, PortalVersionID: value.ApplicationRevisionID,
		Status: value.Status, PreviousReleaseID: value.PreviousActivationID, Resolved: cloneSpec(value.Spec), ArtifactReferences: cloneJSON(value.ArtifactReferences),
		ReferencePending: value.ReferencePending, Phases: cloneJSON(value.Phases), ActorID: value.ActorID, Reason: value.Reason, CreatedAt: value.CreatedAt,
	}
}
