package deploymentmanager

import (
	"context"
	"errors"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformprofileactivation"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) rollbackProfileActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant, candidateID string, cause error) (platformprofileactivation.Activation, error) {
	s.mu.Lock()
	state := s.tenantLocked(tenant)
	record, ok := state.ProfileActivations[candidateID]
	if !ok {
		s.mu.Unlock()
		return platformprofileactivation.Activation{}, errNotFound
	}
	if record.Status == platformprofileactivation.ActivationRolledBack {
		s.mu.Unlock()
		return publicProfileActivation(record), nil
	}
	if record.Status != platformprofileactivation.ActivationCatalogActive && record.Status != platformprofileactivation.ActivationPublishing && record.Status != platformprofileactivation.ActivationRollingBack {
		s.mu.Unlock()
		return platformprofileactivation.Activation{}, errServiceState
	}
	if record.Status != platformprofileactivation.ActivationRollingBack {
		previous := cloneProfileActivation(record)
		record.Status = platformprofileactivation.ActivationRollingBack
		record.ErrorCode, record.ErrorMessage = "platform.plugin_configuration.profile_candidate_not_ready", cause.Error()
		record.UpdatedAt = s.now().Format(time.RFC3339Nano)
		state.ProfileActivations[candidateID] = record
		if err := s.saveLocked(); err != nil {
			state.ProfileActivations[candidateID] = previous
			s.mu.Unlock()
			return platformprofileactivation.Activation{}, err
		}
	}
	s.mu.Unlock()

	key := platformprofileactivation.CandidateRequest{CandidateID: record.CandidateID, RequestDigest: record.RequestDigest}
	var candidate platformprofileactivation.Candidate
	if err := callKernelProfileActivation(ctx, host, call, platformprofileactivation.KernelRollbackService, key, &candidate); err != nil || candidate.Status != platformprofileactivation.StatusRolledBack {
		return platformprofileactivation.Activation{}, errors.Join(errProfileActivation, err)
	}
	if err := s.checkpointProfileRollbackRevision(tenant, candidateID, candidate); err != nil {
		return platformprofileactivation.Activation{}, err
	}

	s.mu.Lock()
	state = s.tenantLocked(tenant)
	record = state.ProfileActivations[candidateID]
	previousService, err := serviceRevisionByID(state, record.PreviousServiceRevision)
	s.mu.Unlock()
	if err != nil {
		return platformprofileactivation.Activation{}, err
	}
	composition, err := normalizeServiceComposition(previousService.Composition, tenant, record.RollbackDeploymentRevision)
	if err != nil {
		return platformprofileactivation.Activation{}, err
	}
	preview, err := previewService(ctx, host, call, composition, record.RollbackDeploymentRevision)
	if err != nil {
		return platformprofileactivation.Activation{}, err
	}
	if err := protectDeploymentTransition(ctx, host, call, record.Deployment, record.RollbackDeploymentRevision*2-1, record.Preview.ArtifactReferences, preview.ArtifactReferences); err != nil {
		return platformprofileactivation.Activation{}, err
	}
	result, err := publishService(ctx, host, call, composition, record.RollbackDeploymentRevision, preview.Digest)
	if err != nil {
		return platformprofileactivation.Activation{}, err
	}
	publisher := actorOrUnknown(call)
	now := s.now().Format(time.RFC3339Nano)
	rollbackRevision := platformadminapi.ServiceRevision{
		ID: record.RollbackDeploymentRevision, Deployment: record.Deployment, Status: platformadminapi.ServicePublished, Active: true,
		Composition: composition, Preview: result.Deployment, PreviewDigest: result.Digest,
		ArtifactReferences: result.ArtifactReferences, ConfigurationCatalog: result.ConfigurationCatalog,
		PreviousServiceRevision: record.DeploymentRevision, KVRevision: result.KVRevision, ReferencePending: true,
		SubmittedBy: record.RequestedBy, ApprovedBy: record.ApprovedBy, PublishedBy: publisher, CreatedAt: now, UpdatedAt: now,
	}
	s.mu.Lock()
	state = s.tenantLocked(tenant)
	current := state.ProfileActivations[candidateID]
	if current.Status != platformprofileactivation.ActivationRollingBack || current.RollbackDeploymentRevision != rollbackRevision.ID {
		s.mu.Unlock()
		return platformprofileactivation.Activation{}, errServiceState
	}
	oldRevisions := cloneServiceRevisions(state.Revisions)
	oldActivation := cloneProfileActivation(current)
	oldAuditLength, oldNextAudit := len(state.ServiceAudit), state.NextAudit
	if _, lookupErr := serviceRevisionIndex(state, rollbackRevision.ID); lookupErr != nil {
		for index := range state.Revisions {
			if state.Revisions[index].Deployment == record.Deployment {
				state.Revisions[index].Active = false
			}
		}
		state.Revisions = append(state.Revisions, rollbackRevision)
		s.auditServiceLocked(state, rollbackRevision, "service.profile_configuration.rolled_back", publisher)
	}
	current.Status, current.CandidateStatus = platformprofileactivation.ActivationRolledBack, candidate.Status
	current.UpdatedAt = now
	state.ProfileActivations[candidateID] = current
	if err := s.saveLocked(); err != nil {
		state.Revisions = oldRevisions
		state.ProfileActivations[candidateID] = oldActivation
		state.ServiceAudit = state.ServiceAudit[:oldAuditLength]
		state.NextAudit = oldNextAudit
		s.mu.Unlock()
		return platformprofileactivation.Activation{}, err
	}
	s.mu.Unlock()
	if err := publishDeploymentReferences(ctx, host, call, record.Deployment, rollbackRevision.ID, result.ArtifactReferences, record.Preview.ArtifactReferences); err == nil {
		s.markServiceReferencesSynced(tenant, rollbackRevision.ID)
	}
	return publicProfileActivation(current), nil
}

func (s *Service) checkpointProfileRollbackRevision(tenant, candidateID string, candidate platformprofileactivation.Candidate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	record, ok := state.ProfileActivations[candidateID]
	if !ok || record.Status != platformprofileactivation.ActivationRollingBack {
		return errServiceState
	}
	previous := cloneProfileActivation(record)
	previousNextRevision := state.NextRevision
	if record.RollbackDeploymentRevision == 0 {
		state.NextRevision++
		record.RollbackDeploymentRevision = state.NextRevision
	}
	record.CandidateStatus = candidate.Status
	record.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.ProfileActivations[candidateID] = record
	if err := s.saveLocked(); err != nil {
		state.ProfileActivations[candidateID] = previous
		state.NextRevision = previousNextRevision
		return err
	}
	return nil
}
