package pluginsettings

import (
	"context"
	"errors"
	"time"

	configurationresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationresource/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) beginResourceAbort(tenant, actor, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	record, recordOK := state.ResourceActivations[id]
	if !ok || !recordOK {
		return pluginconfiguration.Candidate{}, ErrNotFound
	}
	if candidate.Status != pluginconfiguration.CandidatePublishing || candidate.Revision != expectedRevision ||
		(record.Status != resourcePendingApproval && record.Status != resourceApproved) {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	previousCandidate, previousRecord := cloneCandidate(candidate), cloneResourceActivation(record)
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	now := s.now().Format(time.RFC3339Nano)
	record.Status, record.UpdatedAt = resourceAborting, now
	candidate.Status, candidate.ExternalStatus = pluginconfiguration.CandidateRollingBack, string(resourceAborting)
	candidate.Revision, candidate.UpdatedAt = candidate.Revision+1, now
	state.Candidates[id], state.ResourceActivations[id] = candidate, record
	s.auditLocked(state, candidate, "configuration.resource.aborting", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id], state.ResourceActivations[id] = previousCandidate, previousRecord
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}

func (s *Service) continueResourceAbort(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant, actor, id string) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	state := s.tenantLocked(tenant)
	record, ok := state.ResourceActivations[id]
	stages := cloneStages(state.CredentialStages[id])
	s.mu.Unlock()
	if !ok || record.Status != resourceAborting {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	statusRequest := configurationresourcev1.StatusRequest{CollectionID: record.Prepare.CollectionID, ResourceID: record.Prepare.ResourceID, CandidateID: id, RequestDigest: record.RequestDigest}
	observation, err := callResourceObservation(ctx, host, call, record.Target, configurationresourcev1.OperationStatus, statusRequest)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if observation.Candidate != nil && observation.Candidate.Status == configurationresourcev1.StatusCommitted {
		return pluginconfiguration.Candidate{}, errors.New("已提交的配置资源不能终止")
	}
	if observation.Candidate == nil || observation.Candidate.Status != configurationresourcev1.StatusAborted {
		observation, err = callResourceObservation(ctx, host, call, record.Target, configurationresourcev1.OperationAbort, configurationresourcev1.CandidateRequest{CandidateID: id, RequestDigest: record.RequestDigest})
		if err != nil {
			return pluginconfiguration.Candidate{}, err
		}
	}
	if observation.Candidate == nil || observation.Candidate.Status != configurationresourcev1.StatusAborted {
		return pluginconfiguration.Candidate{}, errors.New("configuration.resource.v1 Candidate 未终止")
	}
	if err := abortCredentialStages(ctx, host, call, id, stages); err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	return s.finishResourceAbort(tenant, actor, id)
}

func (s *Service) finishResourceAbort(tenant, actor, id string) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	record, recordOK := state.ResourceActivations[id]
	if !ok || !recordOK || candidate.Status != pluginconfiguration.CandidateRollingBack || record.Status != resourceAborting {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	previousCandidate, previousRecord := cloneCandidate(candidate), cloneResourceActivation(record)
	currentKey := candidateCurrentKey(candidate)
	currentID, hadCurrent := state.Current[currentKey]
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	now := s.now().Format(time.RFC3339Nano)
	record.Status, record.UpdatedAt = resourceAborted, now
	candidate.Status, candidate.ExternalStatus = pluginconfiguration.CandidateRolledBack, string(configurationresourcev1.StatusAborted)
	candidate.Revision, candidate.UpdatedAt = candidate.Revision+1, now
	state.Candidates[id], state.ResourceActivations[id] = candidate, record
	delete(state.Current, currentKey)
	s.auditLocked(state, candidate, "configuration.resource.aborted", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id], state.ResourceActivations[id] = previousCandidate, previousRecord
		if hadCurrent {
			state.Current[currentKey] = currentID
		}
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}
