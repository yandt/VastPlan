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

func (s *Service) beginHotAbort(tenant, actor, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	record, recordOK := state.HotActivations[id]
	if !ok || !recordOK {
		return pluginconfiguration.Candidate{}, ErrNotFound
	}
	if candidate.Status != pluginconfiguration.CandidatePublishing || candidate.Revision != expectedRevision || (record.Status != hotPendingApproval && record.Status != hotApproved) {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	previousCandidate, previousRecord := cloneCandidate(candidate), cloneHotActivation(record)
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	now := s.now().Format(time.RFC3339Nano)
	record.Status, record.UpdatedAt = hotAborting, now
	candidate.Status, candidate.ExternalStatus = pluginconfiguration.CandidateRollingBack, string(hotAborting)
	candidate.Revision, candidate.UpdatedAt = candidate.Revision+1, now
	state.Candidates[id], state.HotActivations[id] = candidate, record
	s.auditLocked(state, candidate, "configuration.hot-service.aborting", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id], state.HotActivations[id] = previousCandidate, previousRecord
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}

func (s *Service) continueHotAbort(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant, actor, id string) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	state := s.tenantLocked(tenant)
	record, ok := state.HotActivations[id]
	stages := cloneStages(state.CredentialStages[id])
	s.mu.Unlock()
	if !ok || record.Status != hotAborting {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	observation, err := callHotController(ctx, host, call, record.Target, configurationv1.OperationAbort, configurationv1.CandidateRequest{CandidateID: id, RequestDigest: record.RequestDigest})
	if err != nil || observation.Candidate == nil || observation.Candidate.Status != configurationv1.StatusAborted {
		return pluginconfiguration.Candidate{}, errors.Join(err, errors.New("configuration.v1 Candidate 未安全终止"))
	}
	if err := abortCredentialStages(ctx, host, call, id, stages); err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	return s.finishHotAbort(tenant, actor, id, observation)
}

func (s *Service) finishHotAbort(tenant, actor, id string, observation configurationv1.Observation) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	record, recordOK := state.HotActivations[id]
	if !ok || !recordOK || candidate.Status != pluginconfiguration.CandidateRollingBack || record.Status != hotAborting {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	previousCandidate, previousRecord := cloneCandidate(candidate), cloneHotActivation(record)
	currentID, hadCurrent := state.Current[candidate.ConfigurationID]
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	now := s.now().Format(time.RFC3339Nano)
	record.Status, record.UpdatedAt = hotAborted, now
	candidate.Status, candidate.ExternalStatus, candidate.ExternalRevision = pluginconfiguration.CandidateRolledBack, string(configurationv1.StatusAborted), observation.Active.Revision
	candidate.ErrorCode, candidate.ErrorMessage = "platform.plugin_configuration.hot_aborted", "Hot Service 配置候选已放弃"
	candidate.Revision, candidate.UpdatedAt = candidate.Revision+1, now
	for index := range candidate.ManagedCredentials {
		candidate.ManagedCredentials[index].Staged = false
		candidate.ManagedCredentials[index].State = "Aborted"
	}
	state.Candidates[id], state.HotActivations[id] = candidate, record
	delete(state.Current, candidate.ConfigurationID)
	s.auditLocked(state, candidate, "configuration.hot-service.aborted", actor)
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
