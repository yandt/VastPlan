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

func (s *Service) beginResourceActivation(tenant, actor, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	record, recordOK := state.ResourceActivations[id]
	if !ok || !recordOK {
		return pluginconfiguration.Candidate{}, ErrNotFound
	}
	if candidate.Status != pluginconfiguration.CandidatePublishing || candidate.Revision != expectedRevision || record.Status != resourceApproved || record.ApprovedBy == "" || record.ApprovedBy == record.SubmittedBy {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	previousCandidate, previousRecord := cloneCandidate(candidate), cloneResourceActivation(record)
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	now := s.now().Format(time.RFC3339Nano)
	record.Status, record.UpdatedAt = resourceActivating, now
	candidate.Status, candidate.ExternalStatus = pluginconfiguration.CandidateActivating, string(resourceActivating)
	candidate.Revision, candidate.UpdatedAt = candidate.Revision+1, now
	state.Candidates[id], state.ResourceActivations[id] = candidate, record
	s.auditLocked(state, candidate, "configuration.resource.activating", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id], state.ResourceActivations[id] = previousCandidate, previousRecord
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}

func (s *Service) continueResourceActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant, actor, id string) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	state := s.tenantLocked(tenant)
	record, ok := state.ResourceActivations[id]
	stages := cloneStages(state.CredentialStages[id])
	s.mu.Unlock()
	if !ok || record.Status != resourceActivating {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	statusRequest := configurationresourcev1.StatusRequest{CollectionID: record.Prepare.CollectionID, ResourceID: record.Prepare.ResourceID, CandidateID: id, RequestDigest: record.RequestDigest}
	observation, err := callResourceObservation(ctx, host, call, record.Target, configurationresourcev1.OperationStatus, statusRequest)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if observation.Candidate == nil || observation.Candidate.CandidateID != id || observation.Candidate.RequestDigest != record.RequestDigest {
		return pluginconfiguration.Candidate{}, errors.New("configuration.resource.v1 status 未返回绑定候选")
	}
	if observation.Candidate.Status == configurationresourcev1.StatusPrepared {
		if !observation.Candidate.Ready {
			return pluginconfiguration.Candidate{}, errors.New("configuration.resource.v1 Candidate 尚未 Ready")
		}
		observation, err = callResourceObservation(ctx, host, call, record.Target, configurationresourcev1.OperationCommit, configurationresourcev1.CandidateRequest{CandidateID: id, RequestDigest: record.RequestDigest})
		if err != nil {
			return pluginconfiguration.Candidate{}, err
		}
	}
	if observation.Candidate == nil || observation.Candidate.Status != configurationresourcev1.StatusCommitted || observation.Candidate.Action != record.Prepare.Action {
		return pluginconfiguration.Candidate{}, errors.New("configuration.resource.v1 Candidate 未提交")
	}
	if err := s.activateCredentialStages(ctx, host, call, tenant, id, stages); err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	return s.finishResourceActivation(tenant, actor, id, observation)
}

func (s *Service) finishResourceActivation(tenant, actor, id string, observation configurationresourcev1.Observation) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	record, recordOK := state.ResourceActivations[id]
	if !ok || !recordOK || candidate.Status != pluginconfiguration.CandidateActivating || record.Status != resourceActivating {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	previousCandidate, previousRecord := cloneCandidate(candidate), cloneResourceActivation(record)
	currentKey := candidateCurrentKey(candidate)
	currentID, hadCurrent := state.Current[currentKey]
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	now := s.now().Format(time.RFC3339Nano)
	record.Status, record.UpdatedAt = resourceReady, now
	candidate.Status, candidate.ExternalStatus = pluginconfiguration.CandidateReady, string(configurationresourcev1.StatusCommitted)
	candidate.ExternalDigest = observation.Candidate.ResultDigest
	if observation.Active != nil {
		candidate.ExternalRevision, candidate.ExternalDigest = observation.Active.Revision, observation.Active.Digest
	} else {
		candidate.ExternalRevision = 0
	}
	candidate.Revision, candidate.UpdatedAt = candidate.Revision+1, now
	state.Candidates[id], state.ResourceActivations[id] = candidate, record
	delete(state.Current, currentKey)
	s.auditLocked(state, candidate, "configuration.resource.ready", actor)
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
