package portalcomposer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func navigationPreparationKey(candidateID, portalID, serviceID string) string {
	return candidateID + ":" + portalID + ":" + serviceID
}

func (s *Service) ReadNavigationConfiguration(_ context.Context, principal portalapi.Principal, portalID, serviceID string) (portalapi.NavigationConfigurationSnapshot, error) {
	if !principal.System || strings.TrimSpace(portalID) == "" || strings.TrimSpace(serviceID) == "" {
		return portalapi.NavigationConfigurationSnapshot{}, ErrForbidden
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	activation, _, profile, binding, ok := s.currentPortalInputsLocked(principal.TenantID, portalID)
	if !ok || !managedServiceExists(binding.Binding.Services, serviceID) {
		return portalapi.NavigationConfigurationSnapshot{}, ErrNotFound
	}
	return portalapi.NavigationConfigurationSnapshot{
		PortalID: portalID, ServiceID: serviceID, ActivationID: activation.ID,
		Folders: navigationFoldersForService(profile.Profile.Shell.Config.NavigationFolders, serviceID),
	}, nil
}

func (s *Service) PrepareNavigationConfiguration(ctx context.Context, principal portalapi.Principal, request portalapi.NavigationConfigurationRequest) (portalapi.NavigationConfigurationPreparation, error) {
	if !principal.System || strings.TrimSpace(request.CandidateID) == "" || strings.TrimSpace(request.PortalID) == "" || strings.TrimSpace(request.ServiceID) == "" || request.ExpectedActivationID == 0 {
		return portalapi.NavigationConfigurationPreparation{}, ErrForbidden
	}
	for _, folder := range request.Folders {
		if folder.ServiceID != request.ServiceID {
			return portalapi.NavigationConfigurationPreparation{}, ErrInvalidState
		}
	}
	requestDigest, err := navigationConfigurationRequestDigest(request)
	if err != nil {
		return portalapi.NavigationConfigurationPreparation{}, ErrInvalidState
	}
	key := navigationPreparationKey(request.CandidateID, request.PortalID, request.ServiceID)
	var version portalapi.PortalVersion
	s.mu.Lock()
	if existing, ok := s.state.NavigationPreparations[key]; ok {
		if existing.PreviousActivationID != request.ExpectedActivationID || existing.RequestDigest != requestDigest {
			s.mu.Unlock()
			return portalapi.NavigationConfigurationPreparation{}, ErrInvalidState
		}
		if existing.Status != portalapi.NavigationConfigurationPreparing {
			s.mu.Unlock()
			return cloneJSON(existing), nil
		}
		index, err := s.revisionIndex(principal.TenantID, existing.VersionID)
		if err != nil {
			s.mu.Unlock()
			return portalapi.NavigationConfigurationPreparation{}, err
		}
		version, err = s.portalVersionLocked(principal.TenantID, s.state.Revisions[index])
		s.mu.Unlock()
		if err != nil {
			return portalapi.NavigationConfigurationPreparation{}, err
		}
	} else {
		activation, application, profile, binding, ok := s.currentPortalInputsLocked(principal.TenantID, request.PortalID)
		if !ok || activation.ID != request.ExpectedActivationID || !managedServiceExists(binding.Binding.Services, request.ServiceID) {
			s.mu.Unlock()
			return portalapi.NavigationConfigurationPreparation{}, ErrInvalidState
		}
		configuration := portalapi.PortalConfiguration{
			Platform: cloneJSON(profile.Profile), Application: cloneComposition(application.Composition), Services: cloneJSON(binding.Binding.Services),
		}
		configuration.Platform.Shell.Config.NavigationFolders = replaceServiceNavigationFolders(configuration.Platform.Shell.Config.NavigationFolders, request.ServiceID, request.Folders)
		number := application.Number
		s.mu.Unlock()
		var err error
		version, err = s.createNavigationVersion(ctx, principal, request, requestDigest, configuration, number, activation.ID)
		if err != nil {
			return portalapi.NavigationConfigurationPreparation{}, err
		}
	}
	published := version
	if version.Status == portalapi.StatusApproved {
		var err error
		published, err = s.transitionPortalVersion(ctx, principal, request.PortalID, version.ID, "publish", true)
		if err != nil {
			return portalapi.NavigationConfigurationPreparation{}, err
		}
	}
	if _, err := s.materializeCatalog(ctx, principal.TenantID, published.Resolved); err != nil {
		return portalapi.NavigationConfigurationPreparation{}, fmt.Errorf("%w: %v", ErrCatalogRejected, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prepared := s.state.NavigationPreparations[key]
	prepared.Status = portalapi.NavigationConfigurationPrepared
	prepared.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)
	s.state.NavigationPreparations[key] = prepared
	if err := s.save(); err != nil {
		return portalapi.NavigationConfigurationPreparation{}, err
	}
	return cloneJSON(prepared), nil
}

func (s *Service) CommitNavigationConfiguration(ctx context.Context, principal portalapi.Principal, lookup portalapi.NavigationConfigurationLookup) (portalapi.NavigationConfigurationPreparation, error) {
	if !principal.System {
		return portalapi.NavigationConfigurationPreparation{}, ErrForbidden
	}
	key := navigationPreparationKey(lookup.CandidateID, lookup.PortalID, lookup.ServiceID)
	s.mu.Lock()
	prepared, ok := s.state.NavigationPreparations[key]
	if !ok {
		s.mu.Unlock()
		return portalapi.NavigationConfigurationPreparation{}, ErrNotFound
	}
	if prepared.Status == portalapi.NavigationConfigurationCommitted {
		s.mu.Unlock()
		return cloneJSON(prepared), nil
	}
	if prepared.Status != portalapi.NavigationConfigurationPrepared || s.currentActivationIDLocked(principal.TenantID, lookup.PortalID) != prepared.PreviousActivationID {
		s.mu.Unlock()
		return portalapi.NavigationConfigurationPreparation{}, ErrInvalidState
	}
	index, err := s.revisionIndex(principal.TenantID, prepared.VersionID)
	if err != nil {
		s.mu.Unlock()
		return portalapi.NavigationConfigurationPreparation{}, err
	}
	revision := s.state.Revisions[index]
	s.mu.Unlock()
	activation, err := s.Activate(ctx, principal, portalapi.ActivationRequest{
		PortalID: lookup.PortalID, ApplicationRevisionID: revision.ID, ProfileRevisionID: revision.ProfileRevisionID,
		BindingRevisionID: revision.BindingRevisionID, ExpectedCurrentID: prepared.PreviousActivationID,
		Reason: "navigation-configuration:" + lookup.CandidateID,
	})
	if err != nil || activation.Status != portalapi.ActivationCurrent {
		if err != nil {
			return portalapi.NavigationConfigurationPreparation{}, err
		}
		return portalapi.NavigationConfigurationPreparation{}, ErrInvalidState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prepared = s.state.NavigationPreparations[key]
	prepared.Status, prepared.ActivationID, prepared.UpdatedAt = portalapi.NavigationConfigurationCommitted, activation.ID, s.now().UTC().Format(time.RFC3339Nano)
	s.state.NavigationPreparations[key] = prepared
	if err := s.save(); err != nil {
		return portalapi.NavigationConfigurationPreparation{}, err
	}
	return cloneJSON(prepared), nil
}

func (s *Service) AbortNavigationConfiguration(_ context.Context, principal portalapi.Principal, lookup portalapi.NavigationConfigurationLookup) (portalapi.NavigationConfigurationPreparation, error) {
	if !principal.System {
		return portalapi.NavigationConfigurationPreparation{}, ErrForbidden
	}
	key := navigationPreparationKey(lookup.CandidateID, lookup.PortalID, lookup.ServiceID)
	s.mu.Lock()
	defer s.mu.Unlock()
	prepared, ok := s.state.NavigationPreparations[key]
	if !ok {
		return portalapi.NavigationConfigurationPreparation{}, ErrNotFound
	}
	if prepared.Status == portalapi.NavigationConfigurationCommitted || prepared.Status == portalapi.NavigationConfigurationRolledBack {
		return portalapi.NavigationConfigurationPreparation{}, ErrInvalidState
	}
	if prepared.Status != portalapi.NavigationConfigurationAborted {
		prepared.Status, prepared.UpdatedAt = portalapi.NavigationConfigurationAborted, s.now().UTC().Format(time.RFC3339Nano)
		s.state.NavigationPreparations[key] = prepared
		if err := s.save(); err != nil {
			return portalapi.NavigationConfigurationPreparation{}, err
		}
	}
	return cloneJSON(prepared), nil
}

func (s *Service) RollbackNavigationConfiguration(ctx context.Context, principal portalapi.Principal, lookup portalapi.NavigationConfigurationLookup) (portalapi.NavigationConfigurationPreparation, error) {
	if !principal.System {
		return portalapi.NavigationConfigurationPreparation{}, ErrForbidden
	}
	key := navigationPreparationKey(lookup.CandidateID, lookup.PortalID, lookup.ServiceID)
	s.mu.Lock()
	prepared, ok := s.state.NavigationPreparations[key]
	if ok && prepared.Status == portalapi.NavigationConfigurationRolledBack {
		s.mu.Unlock()
		return cloneJSON(prepared), nil
	}
	if !ok || prepared.Status != portalapi.NavigationConfigurationCommitted || prepared.ActivationID == 0 || prepared.PreviousActivationID == 0 || s.currentActivationIDLocked(principal.TenantID, lookup.PortalID) != prepared.ActivationID {
		s.mu.Unlock()
		return portalapi.NavigationConfigurationPreparation{}, ErrInvalidState
	}
	s.mu.Unlock()
	activation, err := s.RollbackActivation(ctx, principal, prepared.PreviousActivationID, prepared.ActivationID, "rollback navigation-configuration:"+lookup.CandidateID)
	if err != nil || activation.Status != portalapi.ActivationCurrent {
		if err != nil {
			return portalapi.NavigationConfigurationPreparation{}, err
		}
		return portalapi.NavigationConfigurationPreparation{}, ErrInvalidState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prepared = s.state.NavigationPreparations[key]
	prepared.Status, prepared.UpdatedAt = portalapi.NavigationConfigurationRolledBack, s.now().UTC().Format(time.RFC3339Nano)
	s.state.NavigationPreparations[key] = prepared
	if err := s.save(); err != nil {
		return portalapi.NavigationConfigurationPreparation{}, err
	}
	return cloneJSON(prepared), nil
}

func (s *Service) createNavigationVersion(ctx context.Context, principal portalapi.Principal, request portalapi.NavigationConfigurationRequest, requestDigest string, configuration portalapi.PortalConfiguration, number, previousActivationID uint64) (portalapi.PortalVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := navigationPreparationKey(request.CandidateID, request.PortalID, request.ServiceID)
	configuration, spec, management, err := s.normalizePortalConfiguration(request.PortalID, principal.TenantID, number, s.state.NextRevision+1, configuration)
	if err != nil {
		return portalapi.PortalVersion{}, err
	}
	if err := s.validateCatalog(ctx, principal.TenantID, spec); err != nil {
		return portalapi.PortalVersion{}, fmt.Errorf("%w: %v", ErrCatalogRejected, err)
	}
	digest, err := portalConfigurationDigest(configuration)
	if err != nil {
		return portalapi.PortalVersion{}, err
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	previousRevision, previousGovernance, previousAudit := s.state.NextRevision, s.state.NextGovernance, s.state.NextAudit
	revisionCount, profileCount, bindingCount, auditCount := len(s.state.Revisions), len(s.state.Profiles), len(s.state.Bindings), len(s.state.Audit)
	s.state.NextRevision++
	versionID := s.state.NextRevision
	s.state.NextGovernance++
	profileID := s.state.NextGovernance
	s.state.NextGovernance++
	bindingID := s.state.NextGovernance
	actor := principal.ID
	profile := portalapi.PlatformProfileRevision{ID: profileID, TenantID: principal.TenantID, Status: portalapi.StatusApproved, Profile: configuration.Platform, SubmittedBy: actor, ApprovedBy: actor, CreatedAt: now, UpdatedAt: now}
	binding := portalapi.BindingRevision{ID: bindingID, TenantID: principal.TenantID, PortalID: request.PortalID, ProfileRevisionID: profileID, Status: portalapi.StatusApproved, Binding: management, SubmittedBy: actor, ApprovedBy: actor, CreatedAt: now, UpdatedAt: now}
	revision := portalapi.Revision{ID: versionID, Number: number, TenantID: principal.TenantID, PortalID: request.PortalID, ProfileRevisionID: profileID, BindingRevisionID: bindingID, Status: portalapi.StatusApproved, Composition: configuration.Application, Spec: spec, SubmittedBy: actor, ApprovedBy: actor, CreatedAt: now, UpdatedAt: now}
	s.state.Profiles = append(s.state.Profiles, profile)
	s.state.Bindings = append(s.state.Bindings, binding)
	s.state.Revisions = append(s.state.Revisions, revision)
	s.state.NavigationVersionOwners[versionID] = key
	s.state.NavigationPreparations[key] = portalapi.NavigationConfigurationPreparation{
		CandidateID: request.CandidateID, PortalID: request.PortalID, ServiceID: request.ServiceID,
		Status: portalapi.NavigationConfigurationPreparing, RequestDigest: requestDigest, ConfigurationDigest: digest, VersionID: versionID,
		PreviousActivationID: previousActivationID, UpdatedAt: now,
	}
	s.auditLocked(revision, "portal.version.navigation_configuration_prepared", principal, request.ServiceID, "normal")
	if err := s.save(); err != nil {
		s.state.NextRevision, s.state.NextGovernance, s.state.NextAudit = previousRevision, previousGovernance, previousAudit
		s.state.Revisions, s.state.Profiles, s.state.Bindings, s.state.Audit = s.state.Revisions[:revisionCount], s.state.Profiles[:profileCount], s.state.Bindings[:bindingCount], s.state.Audit[:auditCount]
		delete(s.state.NavigationVersionOwners, versionID)
		delete(s.state.NavigationPreparations, key)
		return portalapi.PortalVersion{}, err
	}
	return s.portalVersionLocked(principal.TenantID, revision)
}

func navigationConfigurationRequestDigest(request portalapi.NavigationConfigurationRequest) (string, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

func managedServiceExists(services []frontendcompositionv1.ManagedService, serviceID string) bool {
	for _, service := range services {
		if service.ID == serviceID {
			return true
		}
	}
	return false
}

func navigationFoldersForService(folders []frontendcompositionv1.NavigationFolder, serviceID string) []frontendcompositionv1.NavigationFolder {
	result := make([]frontendcompositionv1.NavigationFolder, 0)
	for _, folder := range folders {
		if folder.ServiceID == serviceID {
			result = append(result, cloneJSON(folder))
		}
	}
	return result
}

func replaceServiceNavigationFolders(current []frontendcompositionv1.NavigationFolder, serviceID string, replacement []frontendcompositionv1.NavigationFolder) []frontendcompositionv1.NavigationFolder {
	result := make([]frontendcompositionv1.NavigationFolder, 0, len(current)+len(replacement))
	for _, folder := range current {
		if folder.ServiceID != serviceID {
			result = append(result, cloneJSON(folder))
		}
	}
	return append(result, cloneJSON(replacement)...)
}
