package portalcomposer

import (
	"context"
	"fmt"
	"strings"
	"time"

	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func installationPreparationKey(candidateID, portalID string) string {
	return candidateID + ":" + portalID
}

func (s *Service) PreparePluginInstallation(ctx context.Context, principal portalapi.Principal, request portalapi.PluginInstallationRequest) (portalapi.PluginInstallationPreparation, error) {
	if !principal.System || strings.TrimSpace(request.CandidateID) == "" || strings.TrimSpace(request.PortalID) == "" || strings.TrimSpace(request.PluginID) == "" {
		return portalapi.PluginInstallationPreparation{}, ErrForbidden
	}
	if (request.Action == plugininstallation.ActionInstall || request.Action == plugininstallation.ActionUpgrade) && (request.Artifact == nil || request.Artifact.PluginID != request.PluginID || request.Artifact.Version == "") {
		return portalapi.PluginInstallationPreparation{}, ErrInvalidState
	}
	if request.Action == plugininstallation.ActionRemove && request.Artifact != nil {
		return portalapi.PluginInstallationPreparation{}, ErrInvalidState
	}
	key := installationPreparationKey(request.CandidateID, request.PortalID)
	var version portalapi.PortalVersion
	s.mu.Lock()
	if existing, ok := s.state.InstallationPreparations[key]; ok {
		if existing.PluginID != request.PluginID || existing.Action != request.Action || !sameArtifactRef(existing.Artifact, request.Artifact) {
			s.mu.Unlock()
			return portalapi.PluginInstallationPreparation{}, ErrInvalidState
		}
		if existing.Status != portalapi.PluginInstallationPreparing {
			s.mu.Unlock()
			return cloneJSON(existing), nil
		}
		index, indexErr := s.revisionIndex(principal.TenantID, existing.VersionID)
		if indexErr != nil {
			s.mu.Unlock()
			return portalapi.PluginInstallationPreparation{}, indexErr
		}
		version, indexErr = s.portalVersionLocked(principal.TenantID, s.state.Revisions[index])
		s.mu.Unlock()
		if indexErr != nil {
			return portalapi.PluginInstallationPreparation{}, indexErr
		}
	} else {
		activation, application, profile, binding, ok := s.currentPortalInputsLocked(principal.TenantID, request.PortalID)
		if !ok {
			s.mu.Unlock()
			return portalapi.PluginInstallationPreparation{}, ErrNotFound
		}
		configuration := portalapi.PortalConfiguration{
			Platform: cloneJSON(profile.Profile), Application: cloneComposition(application.Composition), Services: cloneJSON(binding.Binding.Services),
		}
		number := application.Number
		s.mu.Unlock()
		if err := mutateApplicationPlugin(&configuration.Application, request); err != nil {
			return portalapi.PluginInstallationPreparation{}, err
		}
		var err error
		version, err = s.createInstallationVersion(ctx, principal, request, configuration, number, activation.ID)
		if err != nil {
			return portalapi.PluginInstallationPreparation{}, err
		}
	}
	published := version
	if version.Status == portalapi.StatusApproved {
		var err error
		published, err = s.transitionPortalVersion(ctx, principal, request.PortalID, version.ID, "publish", true)
		if err != nil {
			return portalapi.PluginInstallationPreparation{}, err
		}
	}
	references, err := s.materializeCatalog(ctx, principal.TenantID, published.Resolved)
	if err != nil {
		return portalapi.PluginInstallationPreparation{}, fmt.Errorf("%w: %v", ErrCatalogRejected, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prepared := s.state.InstallationPreparations[key]
	prepared.Status = portalapi.PluginInstallationPrepared
	prepared.ArtifactReferences = withPortalPurpose(references, "candidate")
	prepared.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)
	s.state.InstallationPreparations[key] = prepared
	if err := s.save(); err != nil {
		return portalapi.PluginInstallationPreparation{}, err
	}
	return cloneJSON(prepared), nil
}

func (s *Service) CommitPluginInstallation(ctx context.Context, principal portalapi.Principal, lookup portalapi.PluginInstallationLookup) (portalapi.PluginInstallationPreparation, error) {
	if !principal.System {
		return portalapi.PluginInstallationPreparation{}, ErrForbidden
	}
	key := installationPreparationKey(lookup.CandidateID, lookup.PortalID)
	s.mu.Lock()
	prepared, ok := s.state.InstallationPreparations[key]
	if !ok {
		s.mu.Unlock()
		return portalapi.PluginInstallationPreparation{}, ErrNotFound
	}
	if prepared.Status == portalapi.PluginInstallationCommitted {
		s.mu.Unlock()
		return cloneJSON(prepared), nil
	}
	if prepared.Status != portalapi.PluginInstallationPrepared || s.currentActivationIDLocked(principal.TenantID, lookup.PortalID) != prepared.PreviousActivationID {
		s.mu.Unlock()
		return portalapi.PluginInstallationPreparation{}, ErrInvalidState
	}
	index, err := s.revisionIndex(principal.TenantID, prepared.VersionID)
	if err != nil {
		s.mu.Unlock()
		return portalapi.PluginInstallationPreparation{}, err
	}
	revision := s.state.Revisions[index]
	s.mu.Unlock()
	activation, err := s.Activate(ctx, principal, portalapi.ActivationRequest{
		PortalID: lookup.PortalID, ApplicationRevisionID: revision.ID, ProfileRevisionID: revision.ProfileRevisionID,
		BindingRevisionID: revision.BindingRevisionID, ExpectedCurrentID: prepared.PreviousActivationID,
		Reason: "plugin-installation:" + lookup.CandidateID,
	})
	if err != nil || activation.Status != portalapi.ActivationCurrent {
		if err != nil {
			return portalapi.PluginInstallationPreparation{}, err
		}
		return portalapi.PluginInstallationPreparation{}, ErrInvalidState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prepared = s.state.InstallationPreparations[key]
	prepared.Status, prepared.ActivationID, prepared.UpdatedAt = portalapi.PluginInstallationCommitted, activation.ID, s.now().UTC().Format(time.RFC3339Nano)
	s.state.InstallationPreparations[key] = prepared
	if err := s.save(); err != nil {
		return portalapi.PluginInstallationPreparation{}, err
	}
	return cloneJSON(prepared), nil
}

func (s *Service) AbortPluginInstallation(_ context.Context, principal portalapi.Principal, lookup portalapi.PluginInstallationLookup) (portalapi.PluginInstallationPreparation, error) {
	if !principal.System {
		return portalapi.PluginInstallationPreparation{}, ErrForbidden
	}
	key := installationPreparationKey(lookup.CandidateID, lookup.PortalID)
	s.mu.Lock()
	defer s.mu.Unlock()
	prepared, ok := s.state.InstallationPreparations[key]
	if !ok {
		return portalapi.PluginInstallationPreparation{}, ErrNotFound
	}
	if prepared.Status == portalapi.PluginInstallationCommitted || prepared.Status == portalapi.PluginInstallationRolledBack {
		return portalapi.PluginInstallationPreparation{}, ErrInvalidState
	}
	if prepared.Status == portalapi.PluginInstallationAborted {
		return cloneJSON(prepared), nil
	}
	prepared.Status, prepared.UpdatedAt = portalapi.PluginInstallationAborted, s.now().UTC().Format(time.RFC3339Nano)
	s.state.InstallationPreparations[key] = prepared
	if err := s.save(); err != nil {
		return portalapi.PluginInstallationPreparation{}, err
	}
	return cloneJSON(prepared), nil
}

func (s *Service) RollbackPluginInstallation(ctx context.Context, principal portalapi.Principal, lookup portalapi.PluginInstallationLookup) (portalapi.PluginInstallationPreparation, error) {
	if !principal.System {
		return portalapi.PluginInstallationPreparation{}, ErrForbidden
	}
	key := installationPreparationKey(lookup.CandidateID, lookup.PortalID)
	s.mu.Lock()
	prepared, ok := s.state.InstallationPreparations[key]
	if ok && prepared.Status == portalapi.PluginInstallationRolledBack {
		s.mu.Unlock()
		return cloneJSON(prepared), nil
	}
	if !ok || prepared.Status != portalapi.PluginInstallationCommitted || prepared.ActivationID == 0 || prepared.PreviousActivationID == 0 || s.currentActivationIDLocked(principal.TenantID, lookup.PortalID) != prepared.ActivationID {
		s.mu.Unlock()
		return portalapi.PluginInstallationPreparation{}, ErrInvalidState
	}
	s.mu.Unlock()
	activation, err := s.RollbackActivation(ctx, principal, prepared.PreviousActivationID, prepared.ActivationID, "rollback plugin-installation:"+lookup.CandidateID)
	if err != nil || activation.Status != portalapi.ActivationCurrent {
		if err != nil {
			return portalapi.PluginInstallationPreparation{}, err
		}
		return portalapi.PluginInstallationPreparation{}, ErrInvalidState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prepared = s.state.InstallationPreparations[key]
	prepared.Status, prepared.UpdatedAt = portalapi.PluginInstallationRolledBack, s.now().UTC().Format(time.RFC3339Nano)
	s.state.InstallationPreparations[key] = prepared
	if err := s.save(); err != nil {
		return portalapi.PluginInstallationPreparation{}, err
	}
	return cloneJSON(prepared), nil
}

func (s *Service) createInstallationVersion(ctx context.Context, principal portalapi.Principal, request portalapi.PluginInstallationRequest, configuration portalapi.PortalConfiguration, number, previousActivationID uint64) (portalapi.PortalVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := installationPreparationKey(request.CandidateID, request.PortalID)
	configuration, spec, management, err := s.normalizePortalConfiguration(request.PortalID, principal.TenantID, number, s.state.NextRevision+1, configuration)
	if err != nil {
		return portalapi.PortalVersion{}, err
	}
	if err := s.validateCatalog(ctx, principal.TenantID, spec); err != nil {
		return portalapi.PortalVersion{}, fmt.Errorf("%w: %v", ErrCatalogRejected, err)
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
	actor := "plugin-installation:" + request.CandidateID
	profile := portalapi.PlatformProfileRevision{ID: profileID, TenantID: principal.TenantID, Status: portalapi.StatusApproved, Profile: configuration.Platform, SubmittedBy: actor, ApprovedBy: actor, CreatedAt: now, UpdatedAt: now}
	binding := portalapi.BindingRevision{ID: bindingID, TenantID: principal.TenantID, PortalID: request.PortalID, ProfileRevisionID: profileID, Status: portalapi.StatusApproved, Binding: management, SubmittedBy: actor, ApprovedBy: actor, CreatedAt: now, UpdatedAt: now}
	revision := portalapi.Revision{ID: versionID, Number: number, TenantID: principal.TenantID, PortalID: request.PortalID, ProfileRevisionID: profileID, BindingRevisionID: bindingID, Status: portalapi.StatusApproved, Composition: configuration.Application, Spec: spec, SubmittedBy: actor, ApprovedBy: actor, CreatedAt: now, UpdatedAt: now}
	s.state.Profiles = append(s.state.Profiles, profile)
	s.state.Bindings = append(s.state.Bindings, binding)
	s.state.Revisions = append(s.state.Revisions, revision)
	s.state.InstallationVersionOwners[versionID] = key
	s.state.InstallationPreparations[key] = portalapi.PluginInstallationPreparation{
		CandidateID: request.CandidateID, PortalID: request.PortalID, Status: portalapi.PluginInstallationPreparing,
		PluginID: request.PluginID, Action: request.Action, Artifact: cloneArtifactRef(request.Artifact), VersionID: versionID,
		PreviousActivationID: previousActivationID, UpdatedAt: now,
	}
	s.auditLocked(revision, "portal.version.plugin_installation_prepared", principal, request.CandidateID, "normal")
	if err := s.save(); err != nil {
		s.state.NextRevision, s.state.NextGovernance, s.state.NextAudit = previousRevision, previousGovernance, previousAudit
		s.state.Revisions, s.state.Profiles, s.state.Bindings, s.state.Audit = s.state.Revisions[:revisionCount], s.state.Profiles[:profileCount], s.state.Bindings[:bindingCount], s.state.Audit[:auditCount]
		delete(s.state.InstallationVersionOwners, versionID)
		delete(s.state.InstallationPreparations, key)
		return portalapi.PortalVersion{}, err
	}
	return s.portalVersionLocked(principal.TenantID, revision)
}

func mutateApplicationPlugin(application *frontendcompositionv1.ApplicationComposition, request portalapi.PluginInstallationRequest) error {
	index := -1
	for candidate := range application.Plugins {
		if application.Plugins[candidate].ID == request.PluginID {
			index = candidate
			break
		}
	}
	switch request.Action {
	case plugininstallation.ActionInstall:
		if index >= 0 {
			return ErrInvalidState
		}
		application.Plugins = append(application.Plugins, frontendcompositionv1.PluginRef{ID: request.Artifact.PluginID, Version: request.Artifact.Version, Channel: request.Artifact.Channel})
	case plugininstallation.ActionUpgrade:
		if index < 0 {
			return ErrInvalidState
		}
		application.Plugins[index] = frontendcompositionv1.PluginRef{ID: request.Artifact.PluginID, Version: request.Artifact.Version, Channel: request.Artifact.Channel}
	case plugininstallation.ActionRemove:
		if index < 0 {
			return ErrInvalidState
		}
		application.Plugins = append(application.Plugins[:index], application.Plugins[index+1:]...)
	default:
		return ErrInvalidState
	}
	return nil
}

func sameArtifactRef(left, right *pluginv1.ArtifactRef) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func cloneArtifactRef(value *pluginv1.ArtifactRef) *pluginv1.ArtifactRef {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
