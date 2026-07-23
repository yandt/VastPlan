package pluginsettings

import (
	"context"
	"errors"
	"fmt"
	"time"

	configurationscopedv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationscoped/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) SubmitScopedDraft(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || id == "" || expectedRevision == 0 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	candidate, err := s.scopedCandidateSnapshot(tenant, id, expectedRevision, pluginconfiguration.CandidateDraft)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	definition, err := s.currentScopedDefinition(ctx, host, call, candidate)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if len(definition.ManagedCredentials) != 0 {
		return pluginconfiguration.Candidate{}, fmt.Errorf("%w: Scoped Hot 托管凭证尚未开放", ErrInvalid)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	base, baseOK := state.ScopedDraftBases[id]
	if !ok || !baseOK || candidate.Status != pluginconfiguration.CandidateDraft || candidate.Revision != expectedRevision {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	previousCandidate := cloneCandidate(candidate)
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	now := s.now().Format(time.RFC3339Nano)
	record := scopedActivationRecord{Base: base, Status: scopedPendingApproval, SubmittedBy: actor, CreatedAt: now, UpdatedAt: now}
	candidate.Status, candidate.ExternalStatus = pluginconfiguration.CandidatePublishing, string(scopedPendingApproval)
	candidate.Revision, candidate.UpdatedAt = candidate.Revision+1, now
	state.Candidates[id], state.ScopedActivations[id] = candidate, record
	delete(state.ScopedDraftBases, id)
	s.auditLocked(state, candidate, "configuration.hot-scoped.pending-approval", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id] = previousCandidate
		delete(state.ScopedActivations, id)
		state.ScopedDraftBases[id] = base
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}

func (s *Service) ApproveScopedCandidate(call *contractv1.CallContext, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || id == "" || expectedRevision == 0 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	record, recordOK := state.ScopedActivations[id]
	if !ok || !recordOK || candidate.Status != pluginconfiguration.CandidatePublishing || candidate.Revision != expectedRevision || record.Status != scopedPendingApproval {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	if actor == record.SubmittedBy || actor == candidate.CreatedBy {
		return pluginconfiguration.Candidate{}, errors.New("Scoped Hot 配置必须由不同主体审批")
	}
	previousCandidate, previousRecord := cloneCandidate(candidate), record
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	now := s.now().Format(time.RFC3339Nano)
	record.Status, record.ApprovedBy, record.UpdatedAt = scopedApproved, actor, now
	candidate.ExternalStatus, candidate.Revision, candidate.UpdatedAt = string(scopedApproved), candidate.Revision+1, now
	state.Candidates[id], state.ScopedActivations[id] = candidate, record
	s.auditLocked(state, candidate, "configuration.hot-scoped.approved", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id], state.ScopedActivations[id] = previousCandidate, previousRecord
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}

func (s *Service) ActivateScopedCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || id == "" || expectedRevision == 0 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	candidate, err := s.scopedCandidateSnapshot(tenant, id, expectedRevision, pluginconfiguration.CandidatePublishing)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	definition, err := s.currentScopedDefinition(ctx, host, call, candidate)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	digest, err := configurationscopedv1.DigestValues(candidate.Values)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	record, recordOK := state.ScopedActivations[id]
	if !ok || !recordOK || candidate.Status != pluginconfiguration.CandidatePublishing || candidate.Revision != expectedRevision || record.Status != scopedApproved {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	key := scopedRecordKey(candidate.ConfigurationID, candidate.ScopeSubjectID)
	current := scopedActiveReference{}
	if active, exists := state.ScopedActives[key]; exists {
		current = active.reference()
	} else {
		seedDigest, digestErr := configurationscopedv1.DigestValues(definition.Values)
		if digestErr != nil {
			return pluginconfiguration.Candidate{}, digestErr
		}
		current.Digest = seedDigest
	}
	if current != record.Base {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	previousCandidate := cloneCandidate(candidate)
	previousActive, hadActive := state.ScopedActives[key]
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	now := s.now().Format(time.RFC3339Nano)
	active := scopedActiveRecord{
		ConfigurationID: candidate.ConfigurationID, PluginID: definition.PluginID, Scope: definition.Scope, SubjectID: candidate.ScopeSubjectID,
		Revision: current.Revision + 1, Digest: digest, SchemaDigest: candidate.SchemaDigest, ArtifactSHA256: candidate.ArtifactSHA256,
		Values: append([]byte(nil), candidate.Values...), CandidateID: candidate.ID, UpdatedAt: now,
	}
	candidate.Status, candidate.ExternalStatus = pluginconfiguration.CandidateReady, "Ready"
	candidate.ExternalRevision, candidate.ExternalDigest = active.Revision, active.Digest
	candidate.Revision, candidate.UpdatedAt = candidate.Revision+1, now
	state.ScopedActives[key], state.Candidates[id] = active, candidate
	delete(state.ScopedActivations, id)
	delete(state.Current, candidateCurrentKey(candidate))
	s.auditLocked(state, candidate, "configuration.hot-scoped.ready", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id] = previousCandidate
		state.ScopedActivations[id] = record
		state.Current[candidateCurrentKey(candidate)] = id
		if hadActive {
			state.ScopedActives[key] = previousActive
		} else {
			delete(state.ScopedActives, key)
		}
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, err
	}
	s.notifyScopedChangedLocked(tenant, key)
	return cloneCandidate(candidate), nil
}

func (s *Service) AbortScopedCandidate(call *contractv1.CallContext, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || id == "" || expectedRevision == 0 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	record, recordOK := state.ScopedActivations[id]
	if !ok || !recordOK || candidate.Status != pluginconfiguration.CandidatePublishing || candidate.Revision != expectedRevision {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	previousCandidate := cloneCandidate(candidate)
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	now := s.now().Format(time.RFC3339Nano)
	candidate.Status, candidate.ExternalStatus = pluginconfiguration.CandidateRolledBack, "Aborted"
	candidate.ErrorCode, candidate.ErrorMessage = "platform.plugin_configuration.scoped_aborted", "Scoped Hot 候选已放弃"
	candidate.Revision, candidate.UpdatedAt = candidate.Revision+1, now
	state.Candidates[id] = candidate
	delete(state.ScopedActivations, id)
	delete(state.Current, candidateCurrentKey(candidate))
	s.auditLocked(state, candidate, "configuration.hot-scoped.aborted", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id], state.ScopedActivations[id] = previousCandidate, record
		state.Current[candidateCurrentKey(candidate)] = id
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}

func (s *Service) scopedCandidateSnapshot(tenant, id string, revision uint64, status pluginconfiguration.CandidateStatus) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate, ok := s.tenantLocked(tenant).Candidates[id]
	if !ok {
		return pluginconfiguration.Candidate{}, ErrNotFound
	}
	if candidate.ApplyPath != pluginconfiguration.ApplyHotScoped || candidate.Revision != revision || candidate.Status != status {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	return cloneCandidate(candidate), nil
}

func (s *Service) currentScopedDefinition(ctx context.Context, host sdk.Host, call *contractv1.CallContext, candidate pluginconfiguration.Candidate) (pluginconfiguration.Definition, error) {
	catalogs, err := s.catalogs(ctx, host, call)
	if err != nil {
		return pluginconfiguration.Definition{}, err
	}
	view, err := findDefinition(catalogs, candidate.ConfigurationID, candidate.CatalogDigest)
	if err != nil {
		return pluginconfiguration.Definition{}, err
	}
	definition := view.Definition
	if definition.ApplyPath != pluginconfiguration.ApplyHotScoped || definition.SchemaDigest != candidate.SchemaDigest || definition.Artifact.SHA256 != candidate.ArtifactSHA256 {
		return pluginconfiguration.Definition{}, ErrConflict
	}
	if _, err := scopedSubject(definition, candidate.ScopeSubjectID); err != nil {
		return pluginconfiguration.Definition{}, ErrInvalid
	}
	if err := pluginconfiguration.ValidateValues(definition, candidate.Values); err != nil {
		return pluginconfiguration.Definition{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return definition, nil
}
