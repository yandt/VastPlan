package pluginsettings

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	configurationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configuration/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) SubmitHotServiceDraft(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
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
	if definition.ApplyPath != pluginconfiguration.ApplyHotService || definition.Controller == nil || definition.Controller.Protocol != configurationv1.Protocol {
		return pluginconfiguration.Candidate{}, fmt.Errorf("%w: 目标插件未实现 configuration.v1", ErrInvalid)
	}
	s.mu.Lock()
	base, hasBase := s.tenantLocked(tenant).HotDraftBases[id]
	s.mu.Unlock()
	if !hasBase {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	active, err := getHotControllerStatus(ctx, host, call, *definition.Controller, configurationv1.StatusRequest{ConfigurationID: definition.ID})
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if active.Active != base {
		return pluginconfiguration.Candidate{}, fmt.Errorf("%w: 目标 Active 已在草稿创建后变化", ErrConflict)
	}
	s.mu.Lock()
	stages := cloneStages(s.tenantLocked(tenant).CredentialStages[id])
	s.mu.Unlock()
	prepare := configurationv1.PrepareRequest{
		CandidateID: draft.ID, ConfigurationID: draft.ConfigurationID, CatalogDigest: draft.CatalogDigest,
		SchemaDigest: draft.SchemaDigest, ArtifactSHA256: draft.ArtifactSHA256, ExpectedActive: base,
		Values: append(json.RawMessage(nil), draft.Values...), ManagedCredentials: hotCredentialRefs(stages),
	}
	requestDigest, err := configurationv1.DigestPrepareRequest(prepare)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	candidate, record, stages, err := s.beginHotSubmission(tenant, actor, id, expectedRevision, *definition.Controller, prepare, requestDigest)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if err := s.prepareCredentialStages(ctx, host, call, tenant, candidate.ID, stages); err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	observation, err := callHotController(ctx, host, call, record.Target, configurationv1.OperationPrepare, record.Prepare)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if err := validateHotPrepared(record, observation); err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	return s.checkpointHotPendingApproval(tenant, actor, candidate.ID, observation)
}

func (s *Service) ApproveHotServiceCandidate(call *contractv1.CallContext, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || id == "" || expectedRevision == 0 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	record, recordOK := state.HotActivations[id]
	if !ok || !recordOK {
		return pluginconfiguration.Candidate{}, ErrNotFound
	}
	if candidate.Status != pluginconfiguration.CandidatePublishing || candidate.Revision != expectedRevision || record.Status != hotPendingApproval || record.SubmittedBy == actor {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	previousCandidate, previousRecord := cloneCandidate(candidate), cloneHotActivation(record)
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	record.Status, record.ApprovedBy, record.UpdatedAt = hotApproved, actor, s.now().Format(time.RFC3339Nano)
	candidate.ExternalStatus, candidate.Revision, candidate.UpdatedAt = string(hotApproved), candidate.Revision+1, record.UpdatedAt
	state.HotActivations[id], state.Candidates[id] = record, candidate
	s.auditLocked(state, candidate, "configuration.hot-service.approved", actor)
	if err := s.saveLocked(); err != nil {
		state.HotActivations[id], state.Candidates[id] = previousRecord, previousCandidate
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}

func (s *Service) ActivateHotServiceCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || id == "" || expectedRevision == 0 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	candidate, _, err := s.beginHotActivation(tenant, actor, id, expectedRevision)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	return s.continueHotActivation(ctx, host, call, tenant, actor, candidate.ID)
}

func (s *Service) AbortHotServiceCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || id == "" || expectedRevision == 0 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	if _, err := s.beginHotAbort(tenant, actor, id, expectedRevision); err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	return s.continueHotAbort(ctx, host, call, tenant, actor, id)
}

func (s *Service) beginHotSubmission(tenant, actor, id string, expectedRevision uint64, target pluginconfiguration.ControllerTarget, prepare configurationv1.PrepareRequest, requestDigest string) (pluginconfiguration.Candidate, hotActivationRecord, []credentialStage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	if !ok {
		return pluginconfiguration.Candidate{}, hotActivationRecord{}, nil, ErrNotFound
	}
	if candidate.Status != pluginconfiguration.CandidateDraft || candidate.Revision != expectedRevision {
		return pluginconfiguration.Candidate{}, hotActivationRecord{}, nil, ErrConflict
	}
	previousCandidate := cloneCandidate(candidate)
	previousRecord, hadRecord := state.HotActivations[id]
	base, hadBase := state.HotDraftBases[id]
	if !hadBase || base != prepare.ExpectedActive {
		return pluginconfiguration.Candidate{}, hotActivationRecord{}, nil, ErrConflict
	}
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	now := s.now().Format(time.RFC3339Nano)
	record := hotActivationRecord{
		Target: target, Prepare: prepare, RequestDigest: requestDigest, Status: hotPreparing,
		SubmittedBy: actor, CreatedAt: now, UpdatedAt: now,
	}
	candidate.Status, candidate.ExternalRevision, candidate.ExternalStatus = pluginconfiguration.CandidatePublishing, prepare.ExpectedActive.Revision, string(hotPreparing)
	candidate.Revision, candidate.UpdatedAt = candidate.Revision+1, now
	state.Candidates[id], state.HotActivations[id] = candidate, record
	delete(state.HotDraftBases, id)
	s.auditLocked(state, candidate, "configuration.hot-service.preparing", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id] = previousCandidate
		if hadRecord {
			state.HotActivations[id] = previousRecord
		} else {
			delete(state.HotActivations, id)
		}
		state.HotDraftBases[id] = base
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, hotActivationRecord{}, nil, err
	}
	return cloneCandidate(candidate), cloneHotActivation(record), cloneStages(state.CredentialStages[id]), nil
}

func (s *Service) checkpointHotPendingApproval(tenant, actor, id string, observation configurationv1.Observation) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	record, recordOK := state.HotActivations[id]
	if !ok || !recordOK || candidate.Status != pluginconfiguration.CandidatePublishing || record.Status != hotPreparing {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	previousCandidate, previousRecord := cloneCandidate(candidate), cloneHotActivation(record)
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	now := s.now().Format(time.RFC3339Nano)
	record.Status, record.UpdatedAt = hotPendingApproval, now
	candidate.ExternalRevision, candidate.ExternalStatus = observation.Active.Revision, string(configurationv1.StatusPrepared)
	candidate.Revision, candidate.UpdatedAt = candidate.Revision+1, now
	state.Candidates[id], state.HotActivations[id] = candidate, record
	s.auditLocked(state, candidate, "configuration.hot-service.pending-approval", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id], state.HotActivations[id] = previousCandidate, previousRecord
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}
