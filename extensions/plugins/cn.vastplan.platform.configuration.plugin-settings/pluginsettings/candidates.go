package pluginsettings

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
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
	id, err := s.newID()
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	now := s.now().Format(time.RFC3339Nano)
	candidate := pluginconfiguration.Candidate{
		ID: id, ConfigurationID: definition.ID, Revision: 1, Status: pluginconfiguration.CandidateDraft,
		CatalogDigest: request.CatalogDigest, SchemaDigest: definition.SchemaDigest, ArtifactSHA256: definition.Artifact.SHA256,
		Values: append(json.RawMessage(nil), request.Values...), CreatedBy: actor, CreatedAt: now, UpdatedAt: now,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	if len(state.Candidates) >= maxCandidates {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	if currentID := state.Current[definition.ID]; currentID != "" {
		if current, ok := state.Candidates[currentID]; ok && !terminal(current.Status) {
			return pluginconfiguration.Candidate{}, ErrConflict
		}
	}
	state.Candidates[id], state.Current[definition.ID] = candidate, id
	s.auditLocked(state, candidate, "configuration.draft.created", actor)
	if err := s.saveLocked(); err != nil {
		delete(state.Candidates, id)
		delete(state.Current, definition.ID)
		state.Audit = state.Audit[:len(state.Audit)-1]
		state.NextAudit--
		return pluginconfiguration.Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}

func (s *Service) DiscardDraft(call *contractv1.CallContext, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || id == "" || expectedRevision == 0 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	if !ok {
		return pluginconfiguration.Candidate{}, ErrNotFound
	}
	if candidate.Status != pluginconfiguration.CandidateDraft || candidate.Revision != expectedRevision {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	old := candidate
	candidate.Status, candidate.Revision, candidate.UpdatedAt = pluginconfiguration.CandidateRolledBack, candidate.Revision+1, s.now().Format(time.RFC3339Nano)
	candidate.ErrorCode, candidate.ErrorMessage = "platform.plugin_configuration.discarded", "候选已由操作者放弃"
	state.Candidates[id] = candidate
	delete(state.Current, candidate.ConfigurationID)
	s.auditLocked(state, candidate, "configuration.draft.discarded", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id], state.Current[candidate.ConfigurationID] = old, id
		state.Audit = state.Audit[:len(state.Audit)-1]
		state.NextAudit--
		return pluginconfiguration.Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}

func (s *Service) auditLocked(state *tenantState, candidate pluginconfiguration.Candidate, action, actor string) {
	state.NextAudit++
	state.Audit = append(state.Audit, AuditEvent{ID: state.NextAudit, CandidateID: candidate.ID, ConfigurationID: candidate.ConfigurationID, Action: action, ActorID: actor, At: s.now().Format(time.RFC3339Nano)})
}

func cloneCandidate(candidate pluginconfiguration.Candidate) pluginconfiguration.Candidate {
	candidate.Values = append(json.RawMessage(nil), candidate.Values...)
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
