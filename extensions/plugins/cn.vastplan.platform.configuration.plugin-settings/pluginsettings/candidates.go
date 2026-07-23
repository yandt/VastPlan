package pluginsettings

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) ListCandidates(call *contractv1.CallContext) ([]pluginconfiguration.Candidate, error) {
	tenant, _, err := tenantAndActor(call)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	items := make([]pluginconfiguration.Candidate, 0, len(state.Candidates))
	for _, candidate := range state.Candidates {
		items = append(items, cloneCandidate(candidate))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt > items[j].UpdatedAt })
	return items, nil
}

func (s *Service) CreateDraft(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request pluginconfiguration.CreateDraftRequest) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || request.ConfigurationID == "" || len(request.CatalogDigest) != 64 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	catalogs, err := s.catalogs(ctx, host, call)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	definitionView, err := findDefinition(catalogs, request.ConfigurationID, request.CatalogDigest)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	definition := definitionView.Definition
	if err := pluginconfiguration.ValidateValues(definition, request.Values); err != nil {
		return pluginconfiguration.Candidate{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	credentialStatus, err := credentialStatuses(definition, request.Secrets)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	id, err := s.newID()
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	now := s.now().Format(time.RFC3339Nano)
	status := pluginconfiguration.CandidateDraft
	if len(request.Secrets) > 0 {
		status = pluginconfiguration.CandidatePreparing
	}
	candidate := pluginconfiguration.Candidate{
		ID: id, ConfigurationID: definition.ID, Revision: 1, Status: status,
		CatalogDigest: request.CatalogDigest, SchemaDigest: definition.SchemaDigest, ArtifactSHA256: definition.Artifact.SHA256,
		Values: append(json.RawMessage(nil), request.Values...), CreatedBy: actor, CreatedAt: now, UpdatedAt: now,
		ManagedCredentials: credentialStatus,
	}
	s.mu.Lock()
	state := s.tenantLocked(tenant)
	if len(state.Candidates) >= maxCandidates {
		s.mu.Unlock()
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	if currentID := state.Current[definition.ID]; currentID != "" {
		if current, ok := state.Candidates[currentID]; ok && !terminal(current.Status) {
			s.mu.Unlock()
			return pluginconfiguration.Candidate{}, ErrConflict
		}
	}
	state.Candidates[id], state.Current[definition.ID] = candidate, id
	state.CredentialStages[id] = map[string]credentialStage{}
	action := "configuration.draft.created"
	if status == pluginconfiguration.CandidatePreparing {
		action = "configuration.credentials.preparing"
	}
	s.auditLocked(state, candidate, action, actor)
	if err := s.saveLocked(); err != nil {
		delete(state.Candidates, id)
		delete(state.Current, definition.ID)
		delete(state.CredentialStages, id)
		state.Audit = state.Audit[:len(state.Audit)-1]
		state.NextAudit--
		s.mu.Unlock()
		return pluginconfiguration.Candidate{}, err
	}
	s.mu.Unlock()
	if len(request.Secrets) == 0 {
		return cloneCandidate(candidate), nil
	}
	staged, stageErr := s.stageSecrets(ctx, host, call, definition, id, request.CatalogDigest, request.Secrets, func(fieldID string, stage pluginconfig.StagedCredential) error {
		return s.checkpointCredential(tenant, id, fieldID, stage)
	})
	if stageErr != nil {
		abortErr := abortCredentialStages(ctx, host, call, id, staged)
		return pluginconfiguration.Candidate{}, errors.Join(stageErr, s.failPreparing(tenant, id, actor, abortErr))
	}
	return s.finishPreparing(tenant, id, actor)
}

func (s *Service) DiscardDraft(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || id == "" || expectedRevision == 0 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	s.mu.Lock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	if !ok {
		s.mu.Unlock()
		return pluginconfiguration.Candidate{}, ErrNotFound
	}
	if candidate.Status != pluginconfiguration.CandidateDraft || candidate.Revision != expectedRevision {
		s.mu.Unlock()
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	old := cloneCandidate(candidate)
	candidate.Status, candidate.Revision, candidate.UpdatedAt = pluginconfiguration.CandidateRollingBack, candidate.Revision+1, s.now().Format(time.RFC3339Nano)
	state.Candidates[id] = candidate
	s.auditLocked(state, candidate, "configuration.draft.discarding", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id] = old
		state.Audit = state.Audit[:len(state.Audit)-1]
		state.NextAudit--
		s.mu.Unlock()
		return pluginconfiguration.Candidate{}, err
	}
	bindings := cloneStages(state.CredentialStages[id])
	s.mu.Unlock()
	if err := abortCredentialStages(ctx, host, call, id, bindings); err != nil {
		return pluginconfiguration.Candidate{}, errors.Join(err, s.failPreparing(tenant, id, actor, err))
	}
	return s.completeRollback(tenant, id, actor)
}

func (s *Service) checkpointCredential(tenant, candidateID, fieldID string, stage pluginconfig.StagedCredential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[candidateID]
	if !ok || candidate.Status != pluginconfiguration.CandidatePreparing {
		return ErrConflict
	}
	previous := cloneCandidate(candidate)
	state.CredentialStages[candidateID][fieldID] = credentialStage{FieldID: fieldID, Stage: stage, State: "Staged"}
	for index := range candidate.ManagedCredentials {
		if candidate.ManagedCredentials[index].FieldID == fieldID {
			candidate.ManagedCredentials[index].Staged = true
			candidate.ManagedCredentials[index].State = "Staged"
		}
	}
	candidate.Revision++
	candidate.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.Candidates[candidateID] = candidate
	if err := s.saveLocked(); err != nil {
		delete(state.CredentialStages[candidateID], fieldID)
		state.Candidates[candidateID] = previous
		return err
	}
	return nil
}

func (s *Service) finishPreparing(tenant, candidateID, actor string) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[candidateID]
	if !ok || candidate.Status != pluginconfiguration.CandidatePreparing {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	previous := cloneCandidate(candidate)
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	candidate.Status, candidate.Revision, candidate.UpdatedAt = pluginconfiguration.CandidateDraft, candidate.Revision+1, s.now().Format(time.RFC3339Nano)
	state.Candidates[candidateID] = candidate
	s.auditLocked(state, candidate, "configuration.credentials.staged", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[candidateID] = previous
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}

func (s *Service) failPreparing(tenant, candidateID, actor string, cause error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[candidateID]
	if !ok {
		return ErrNotFound
	}
	previous := cloneCandidate(candidate)
	currentID, hadCurrent := state.Current[candidate.ConfigurationID]
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	candidate.Status, candidate.Revision, candidate.UpdatedAt = pluginconfiguration.CandidateFailed, candidate.Revision+1, s.now().Format(time.RFC3339Nano)
	candidate.ErrorCode = "platform.plugin_configuration.credential_stage_failed"
	candidate.ErrorMessage = "托管凭证暂存或回滚失败"
	if cause == nil {
		candidate.ErrorMessage = "托管凭证暂存失败，已回滚"
	}
	state.Candidates[candidateID] = candidate
	delete(state.Current, candidate.ConfigurationID)
	s.auditLocked(state, candidate, "configuration.credentials.failed", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[candidateID] = previous
		if hadCurrent {
			state.Current[candidate.ConfigurationID] = currentID
		}
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return err
	}
	return nil
}

func (s *Service) completeRollback(tenant, candidateID, actor string) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[candidateID]
	if !ok || candidate.Status != pluginconfiguration.CandidateRollingBack {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	previous := cloneCandidate(candidate)
	currentID, hadCurrent := state.Current[candidate.ConfigurationID]
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	candidate.Status, candidate.Revision, candidate.UpdatedAt = pluginconfiguration.CandidateRolledBack, candidate.Revision+1, s.now().Format(time.RFC3339Nano)
	candidate.ErrorCode, candidate.ErrorMessage = "platform.plugin_configuration.discarded", "候选已由操作者放弃"
	for index := range candidate.ManagedCredentials {
		candidate.ManagedCredentials[index].Staged = false
		candidate.ManagedCredentials[index].State = "Aborted"
	}
	state.Candidates[candidateID] = candidate
	delete(state.Current, candidate.ConfigurationID)
	s.auditLocked(state, candidate, "configuration.draft.discarded", actor)
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

func abortCredentialStages(ctx context.Context, host sdk.Host, call *contractv1.CallContext, candidateID string, stages []credentialStage) error {
	var result error
	for _, binding := range stages {
		result = errors.Join(result, callCredentials(ctx, host, call, "abortDelegated", map[string]string{"stageId": binding.Stage.ID, "candidateId": candidateID}, nil))
	}
	return result
}

func cloneStages(values map[string]credentialStage) []credentialStage {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]credentialStage, 0, len(keys))
	for _, key := range keys {
		out = append(out, values[key])
	}
	return out
}

func (s *Service) auditLocked(state *tenantState, candidate pluginconfiguration.Candidate, action, actor string) {
	state.NextAudit++
	state.Audit = append(state.Audit, AuditEvent{ID: state.NextAudit, CandidateID: candidate.ID, ConfigurationID: candidate.ConfigurationID, Action: action, ActorID: actor, At: s.now().Format(time.RFC3339Nano)})
}

func cloneCandidate(candidate pluginconfiguration.Candidate) pluginconfiguration.Candidate {
	candidate.Values = append(json.RawMessage(nil), candidate.Values...)
	candidate.ManagedCredentials = append([]pluginconfiguration.ManagedCredentialStatus(nil), candidate.ManagedCredentials...)
	return candidate
}

func terminal(status pluginconfiguration.CandidateStatus) bool {
	switch status {
	case pluginconfiguration.CandidateReady, pluginconfiguration.CandidateFailed, pluginconfiguration.CandidateRolledBack:
		return true
	default:
		return false
	}
}

func randomID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "pcfg_" + hex.EncodeToString(raw[:]), nil
}
