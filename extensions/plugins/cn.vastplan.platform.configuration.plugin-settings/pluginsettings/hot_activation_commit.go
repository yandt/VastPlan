package pluginsettings

import (
	"context"
	"errors"
	"time"

	configurationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configuration/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) beginHotActivation(tenant, actor, id string, expectedRevision uint64) (pluginconfiguration.Candidate, hotActivationRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	record, recordOK := state.HotActivations[id]
	if !ok || !recordOK {
		return pluginconfiguration.Candidate{}, hotActivationRecord{}, ErrNotFound
	}
	if candidate.Status != pluginconfiguration.CandidatePublishing || candidate.Revision != expectedRevision || record.Status != hotApproved || record.ApprovedBy == "" || record.ApprovedBy == record.SubmittedBy {
		return pluginconfiguration.Candidate{}, hotActivationRecord{}, ErrConflict
	}
	previousCandidate, previousRecord := cloneCandidate(candidate), cloneHotActivation(record)
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	now := s.now().Format(time.RFC3339Nano)
	record.Status, record.UpdatedAt = hotActivating, now
	candidate.Status, candidate.ExternalStatus = pluginconfiguration.CandidateActivating, string(hotActivating)
	candidate.Revision, candidate.UpdatedAt = candidate.Revision+1, now
	state.Candidates[id], state.HotActivations[id] = candidate, record
	s.auditLocked(state, candidate, "configuration.hot-service.activating", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id], state.HotActivations[id] = previousCandidate, previousRecord
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, hotActivationRecord{}, err
	}
	return cloneCandidate(candidate), cloneHotActivation(record), nil
}

func (s *Service) continueHotActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant, actor, id string) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	state := s.tenantLocked(tenant)
	record, ok := state.HotActivations[id]
	stages := cloneStages(state.CredentialStages[id])
	s.mu.Unlock()
	if !ok || (record.Status != hotActivating && record.Status != hotFinalizing) {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	observation, err := getHotControllerStatus(ctx, host, call, record.Target, configurationv1.StatusRequest{ConfigurationID: record.Prepare.ConfigurationID, CandidateID: record.Prepare.CandidateID, RequestDigest: record.RequestDigest})
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if observation.Candidate == nil || observation.Candidate.CandidateID != id || observation.Candidate.RequestDigest != record.RequestDigest {
		return pluginconfiguration.Candidate{}, errors.New("configuration.v1 status 未返回绑定候选")
	}
	if observation.Candidate.Status == configurationv1.StatusPrepared {
		if !observation.Candidate.Ready {
			return pluginconfiguration.Candidate{}, errors.New("configuration.v1 Candidate 尚未 Ready")
		}
		observation, err = callHotController(ctx, host, call, record.Target, configurationv1.OperationCommit, configurationv1.CandidateRequest{CandidateID: id, RequestDigest: record.RequestDigest})
		if err != nil {
			return pluginconfiguration.Candidate{}, err
		}
	}
	if observation.Candidate == nil || observation.Candidate.Status != configurationv1.StatusCommitted || observation.Active.Digest != observation.Candidate.ConfigurationDigest {
		return pluginconfiguration.Candidate{}, errors.New("configuration.v1 Candidate 未提交为 Active")
	}
	if record.Status == hotActivating {
		if err := s.checkpointHotControllerCommitted(tenant, actor, id, observation); err != nil {
			return pluginconfiguration.Candidate{}, err
		}
	}
	// Candidate references are leaseable. Committing the controller first keeps
	// a failed commit abortable and avoids producing an unreferenced Active
	// credential. A crash here is recovered from the durable Committed fact.
	if err := s.activateCredentialStages(ctx, host, call, tenant, id, stages); err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	return s.finishHotActivation(tenant, actor, id, observation)
}

func (s *Service) checkpointHotControllerCommitted(tenant, actor, id string, observation configurationv1.Observation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, candidateOK := state.Candidates[id]
	record, recordOK := state.HotActivations[id]
	if !candidateOK || !recordOK || candidate.Status != pluginconfiguration.CandidateActivating || record.Status != hotActivating ||
		observation.Candidate == nil || observation.Candidate.CandidateID != id || observation.Candidate.Status != configurationv1.StatusCommitted {
		return ErrConflict
	}
	previousCandidate, previousRecord := cloneCandidate(candidate), cloneHotActivation(record)
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	now := s.now().Format(time.RFC3339Nano)
	record.Status, record.UpdatedAt = hotFinalizing, now
	candidate.ExternalStatus, candidate.ExternalRevision = string(hotFinalizing), observation.Active.Revision
	candidate.Revision, candidate.UpdatedAt = candidate.Revision+1, now
	state.Candidates[id], state.HotActivations[id] = candidate, record
	s.auditLocked(state, candidate, "configuration.hot-service.controller-committed", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id], state.HotActivations[id] = previousCandidate, previousRecord
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return err
	}
	return nil
}

func (s *Service) finishHotActivation(tenant, actor, id string, observation configurationv1.Observation) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	record, recordOK := state.HotActivations[id]
	if ok && recordOK && candidate.Status == pluginconfiguration.CandidateReady && record.Status == hotReady &&
		candidate.ExternalRevision == observation.Active.Revision && candidate.ExternalStatus == string(configurationv1.StatusCommitted) {
		return cloneCandidate(candidate), nil
	}
	if !ok || !recordOK || candidate.Status != pluginconfiguration.CandidateActivating || record.Status != hotFinalizing {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	previousCandidate, previousRecord := cloneCandidate(candidate), cloneHotActivation(record)
	currentID, hadCurrent := state.Current[candidate.ConfigurationID]
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	now := s.now().Format(time.RFC3339Nano)
	record.Status, record.UpdatedAt = hotReady, now
	candidate.Status, candidate.ExternalStatus, candidate.ExternalRevision = pluginconfiguration.CandidateReady, string(configurationv1.StatusCommitted), observation.Active.Revision
	candidate.Revision, candidate.UpdatedAt = candidate.Revision+1, now
	state.Candidates[id], state.HotActivations[id] = candidate, record
	delete(state.Current, candidate.ConfigurationID)
	s.auditLocked(state, candidate, "configuration.hot-service.ready", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id], state.HotActivations[id] = previousCandidate, previousRecord
		if hadCurrent {
			state.Current[candidate.ConfigurationID] = currentID
		}
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}
