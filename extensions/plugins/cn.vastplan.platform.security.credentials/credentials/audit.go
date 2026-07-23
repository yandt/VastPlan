package credentials

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
)

const maximumManagedAuditEvents = 10000

type ManagedAuditEvent struct {
	ID                    uint64    `json:"id"`
	CredentialFingerprint string    `json:"credentialFingerprint"`
	Action                string    `json:"action"`
	State                 string    `json:"state"`
	Owner                 string    `json:"owner"`
	Purpose               string    `json:"purpose"`
	Resource              string    `json:"resource"`
	Delegated             bool      `json:"delegated"`
	CandidateID           string    `json:"candidateId,omitempty"`
	ConfigurationID       string    `json:"configurationId,omitempty"`
	FieldID               string    `json:"fieldId,omitempty"`
	OccurredAt            time.Time `json:"occurredAt"`
}

type managedAuditState struct {
	NextID uint64              `json:"nextId"`
	Events []ManagedAuditEvent `json:"events"`
}

type ManagedAuditPage struct {
	Items        []ManagedAuditEvent      `json:"items"`
	NextBeforeID uint64                   `json:"nextBeforeId,omitempty"`
	Maintenance  ManagedMaintenanceStatus `json:"maintenance"`
}

func credentialFingerprint(handle string) string {
	digest := sha256.Sum256([]byte(handle))
	return hex.EncodeToString(digest[:16])
}

func managedAuditEventValid(event ManagedAuditEvent) bool {
	return event.ID > 0 && len(event.CredentialFingerprint) == 32 && strings.TrimSpace(event.Action) != "" &&
		strings.TrimSpace(event.State) != "" && strings.TrimSpace(event.Owner) != "" && strings.TrimSpace(event.Purpose) != "" &&
		strings.TrimSpace(event.Resource) != "" && !event.OccurredAt.IsZero()
}

func (s *Service) appendManagedAuditLocked(tenantID, action string, record ManagedRecord, at time.Time) {
	state := s.managedAuditStateLocked(tenantID)
	state.NextID++
	state.Events = append(state.Events, ManagedAuditEvent{
		ID: state.NextID, CredentialFingerprint: credentialFingerprint(record.Ref.Handle), Action: action, State: record.State,
		Owner: record.Ref.Owner, Purpose: record.Ref.Purpose, Resource: record.Resource, Delegated: record.Coordinator != "",
		CandidateID: record.CandidateID, ConfigurationID: record.ConfigurationID, FieldID: record.FieldID, OccurredAt: at,
	})
	if overflow := len(state.Events) - maximumManagedAuditEvents; overflow > 0 {
		state.Events = append([]ManagedAuditEvent(nil), state.Events[overflow:]...)
	}
	s.data.ManagedAudit[tenantID] = state
}

func (s *Service) managedAuditStateLocked(tenantID string) managedAuditState {
	return s.data.ManagedAudit[tenantID]
}

func cloneManagedAuditState(state managedAuditState) managedAuditState {
	state.Events = append([]ManagedAuditEvent(nil), state.Events...)
	return state
}

func (s *Service) ListManagedAudit(call *contractv1.CallContext, beforeID uint64, limit int) (ManagedAuditPage, error) {
	tenantID, err := tenant(call)
	if err != nil {
		return ManagedAuditPage{}, err
	}
	if call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_USER || strings.TrimSpace(call.GetCaller().GetId()) == "" {
		return ManagedAuditPage{}, errors.New("托管凭证审计只接受已认证管理员")
	}
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 200 {
		return ManagedAuditPage{}, errors.New("托管凭证审计 limit 必须为 1-200")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.managedAuditStateLocked(tenantID)
	items := make([]ManagedAuditEvent, 0, limit+1)
	for index := len(state.Events) - 1; index >= 0 && len(items) <= limit; index-- {
		event := state.Events[index]
		if beforeID != 0 && event.ID >= beforeID {
			continue
		}
		items = append(items, event)
	}
	next := uint64(0)
	if len(items) > limit {
		items = items[:limit]
		next = items[len(items)-1].ID
	}
	return ManagedAuditPage{Items: items, NextBeforeID: next, Maintenance: s.maintenanceStatusLocked(tenantID)}, nil
}
