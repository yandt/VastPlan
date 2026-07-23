package pluginsettings

import (
	"context"
	"errors"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformprofileactivation"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) SubmitProfileDraft(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || id == "" || expectedRevision == 0 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	draft, err := s.candidateSnapshot(tenant, id, expectedRevision, pluginconfiguration.CandidateDraft)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	definition, err := s.currentDefinition(ctx, host, call, draft)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if definition.ApplyPath != pluginconfiguration.ApplyPlatformProfile {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	candidate, stages, err := s.beginProfileSubmission(tenant, actor, id, expectedRevision)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	activation, err := createProfileActivation(ctx, host, call, definition, candidate, stages)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	return s.checkpointProfileExternal(tenant, actor, candidate.ID, activation, "configuration.profile.submitted")
}

func (s *Service) ApproveProfileCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || id == "" || expectedRevision == 0 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	s.mu.Lock()
	candidate, ok := s.tenantLocked(tenant).Candidates[id]
	s.mu.Unlock()
	if !ok {
		return pluginconfiguration.Candidate{}, ErrNotFound
	}
	if candidate.Status != pluginconfiguration.CandidatePublishing || candidate.Revision != expectedRevision || candidate.ExternalStatus != string(platformprofileactivation.ActivationPendingApproval) {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	activation, err := approveProfileActivation(ctx, host, call, id)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	return s.checkpointProfileExternal(tenant, actor, id, activation, "configuration.profile.approved")
}

func (s *Service) ActivateProfileCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || id == "" || expectedRevision == 0 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	activation, err := getProfileActivation(ctx, host, call, id)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if activation.Status != platformprofileactivation.ActivationApproved {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	candidate, stages, err := s.beginProfileActivation(tenant, actor, id, expectedRevision, activation)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if err := s.prepareCredentialStages(ctx, host, call, tenant, candidate.ID, stages); err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	activation, err = publishProfileActivation(ctx, host, call, candidate.ID)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	return s.completeProfileActivation(ctx, host, call, tenant, actor, candidate.ID, activation)
}

func (s *Service) AbortProfileCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || id == "" || expectedRevision == 0 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	s.mu.Lock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	stages := cloneStages(state.CredentialStages[id])
	s.mu.Unlock()
	if !ok {
		return pluginconfiguration.Candidate{}, ErrNotFound
	}
	if candidate.Status != pluginconfiguration.CandidatePublishing || candidate.Revision != expectedRevision {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	activation, err := abortProfileActivation(ctx, host, call, id)
	if err != nil || activation.Status != platformprofileactivation.ActivationAborted {
		return pluginconfiguration.Candidate{}, errors.Join(err, ErrConflict)
	}
	abortErr := abortCredentialStages(ctx, host, call, id, stages)
	result, finishErr := s.finishProfileExternal(tenant, actor, id, activation, pluginconfiguration.CandidateRolledBack, "configuration.profile.aborted")
	return result, errors.Join(abortErr, finishErr)
}

func (s *Service) beginProfileSubmission(tenant, actor, id string, expectedRevision uint64) (pluginconfiguration.Candidate, []credentialStage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	if !ok {
		return pluginconfiguration.Candidate{}, nil, ErrNotFound
	}
	if candidate.Status != pluginconfiguration.CandidateDraft || candidate.Revision != expectedRevision {
		return pluginconfiguration.Candidate{}, nil, ErrConflict
	}
	previous := cloneCandidate(candidate)
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	candidate.Status, candidate.ExternalStatus = pluginconfiguration.CandidatePublishing, string(platformprofileactivation.ActivationPreparing)
	candidate.Revision++
	candidate.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.Candidates[id] = candidate
	s.auditLocked(state, candidate, "configuration.profile.submitting", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id] = previous
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, nil, err
	}
	return cloneCandidate(candidate), cloneStages(state.CredentialStages[id]), nil
}

func (s *Service) checkpointProfileExternal(tenant, actor, id string, activation platformprofileactivation.Activation, action string) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	if !ok || candidate.Status != pluginconfiguration.CandidatePublishing || (candidate.ExternalRevision != 0 && candidate.ExternalRevision != activation.DeploymentRevision) {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	if candidate.ExternalRevision == activation.DeploymentRevision && candidate.ExternalStatus == string(activation.Status) && candidate.RollbackRevision == activation.RollbackDeploymentRevision {
		return cloneCandidate(candidate), nil
	}
	previous := cloneCandidate(candidate)
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	candidate.ExternalRevision, candidate.ExternalStatus = activation.DeploymentRevision, string(activation.Status)
	candidate.RollbackRevision = activation.RollbackDeploymentRevision
	candidate.Revision++
	candidate.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.Candidates[id] = candidate
	s.auditLocked(state, candidate, action, actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id] = previous
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}

func (s *Service) beginProfileActivation(tenant, actor, id string, expectedRevision uint64, activation platformprofileactivation.Activation) (pluginconfiguration.Candidate, []credentialStage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	if !ok {
		return pluginconfiguration.Candidate{}, nil, ErrNotFound
	}
	if candidate.Status != pluginconfiguration.CandidatePublishing || candidate.Revision != expectedRevision || candidate.ExternalRevision != activation.DeploymentRevision || activation.Status != platformprofileactivation.ActivationApproved {
		return pluginconfiguration.Candidate{}, nil, ErrConflict
	}
	previous := cloneCandidate(candidate)
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	candidate.Status, candidate.ExternalStatus = pluginconfiguration.CandidateActivating, string(activation.Status)
	candidate.Revision++
	candidate.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.Candidates[id] = candidate
	s.auditLocked(state, candidate, "configuration.profile.activating", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id] = previous
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, nil, err
	}
	return cloneCandidate(candidate), cloneStages(state.CredentialStages[id]), nil
}

func (s *Service) completeProfileActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant, actor, candidateID string, activation platformprofileactivation.Activation) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	stages := cloneStages(s.tenantLocked(tenant).CredentialStages[candidateID])
	s.mu.Unlock()
	switch activation.Status {
	case platformprofileactivation.ActivationReady:
		if err := s.activateCredentialStages(ctx, host, call, tenant, candidateID, stages); err != nil {
			return pluginconfiguration.Candidate{}, err
		}
		return s.finishProfileExternal(tenant, actor, candidateID, activation, pluginconfiguration.CandidateReady, "configuration.profile.ready")
	case platformprofileactivation.ActivationRolledBack, platformprofileactivation.ActivationAborted, platformprofileactivation.ActivationFailed:
		abortErr := abortCredentialStages(ctx, host, call, candidateID, stages)
		status := pluginconfiguration.CandidateRolledBack
		if activation.Status == platformprofileactivation.ActivationFailed || abortErr != nil {
			status = pluginconfiguration.CandidateFailed
		}
		candidate, finishErr := s.finishProfileExternal(tenant, actor, candidateID, activation, status, "configuration.profile.rolled_back")
		return candidate, errors.Join(abortErr, finishErr)
	default:
		return s.finishProfileExternal(tenant, actor, candidateID, activation, pluginconfiguration.CandidateActivating, "configuration.profile.progress")
	}
}

func (s *Service) finishProfileExternal(tenant, actor, candidateID string, activation platformprofileactivation.Activation, status pluginconfiguration.CandidateStatus, action string) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[candidateID]
	if !ok {
		return pluginconfiguration.Candidate{}, ErrNotFound
	}
	previous := cloneCandidate(candidate)
	currentID, hadCurrent := state.Current[candidate.ConfigurationID]
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	candidate.Status, candidate.ExternalStatus = status, string(activation.Status)
	candidate.ExternalRevision, candidate.RollbackRevision = activation.DeploymentRevision, activation.RollbackDeploymentRevision
	candidate.ErrorCode, candidate.ErrorMessage = activation.ErrorCode, activation.ErrorMessage
	candidate.Revision++
	candidate.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.Candidates[candidateID] = candidate
	if terminal(status) {
		delete(state.Current, candidate.ConfigurationID)
	}
	s.auditLocked(state, candidate, action, actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[candidateID] = previous
		if hadCurrent {
			state.Current[candidate.ConfigurationID] = currentID
		}
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}
