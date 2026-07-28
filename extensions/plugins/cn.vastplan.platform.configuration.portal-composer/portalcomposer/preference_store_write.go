package portalcomposer

import (
	"errors"
	"fmt"
	"time"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

const maximumPreferenceWriteAttempts = 3

var errPreferenceDocumentConflict = errors.New("PortalPreference Shared State 文档冲突")

type preferenceMutation struct {
	value portalapi.PortalPreference
	next  preferenceState
	write bool
}

func (s *preferenceStore) Put(principal portalapi.Principal, request portalapi.PutPortalPreferenceRequest) (portalapi.PortalPreference, error) {
	if err := validatePreferencePrincipal(principal); err != nil {
		return portalapi.PortalPreference{}, err
	}
	if s.principal.ID != "" && (s.principal.ID != principal.ID || s.principal.TenantID != principal.TenantID) {
		return portalapi.PortalPreference{}, portalapi.ErrForbidden
	}
	if err := portalapi.ValidatePortalPreferenceScope(request.Scope); err != nil {
		return portalapi.PortalPreference{}, err
	}
	if err := portalapi.ValidatePortalPreferenceValues(request.Values); err != nil {
		return portalapi.PortalPreference{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.persist == nil {
		return portalapi.PortalPreference{}, errors.New("PortalPreference 写入缺少 Shared State 会话")
	}
	for attempt := 0; attempt < maximumPreferenceWriteAttempts; attempt++ {
		mutation, err := s.preparePreferenceMutation(principal, request)
		if err != nil {
			return portalapi.PortalPreference{}, err
		}
		if !mutation.write {
			return mutation.value, nil
		}
		revision, err := s.persist(mutation.next, s.revision)
		if err == nil {
			s.state, s.revision = mutation.next, revision
			return clonePortalPreference(mutation.value), nil
		}
		if !errors.Is(err, errPreferenceDocumentConflict) || s.reload == nil {
			return portalapi.PortalPreference{}, err
		}
		if attempt+1 == maximumPreferenceWriteAttempts {
			return portalapi.PortalPreference{}, fmt.Errorf("PortalPreference Shared State 持续争用: %w", err)
		}
		state, revision, err := s.reload()
		if err != nil {
			return portalapi.PortalPreference{}, err
		}
		s.state, s.revision = state, revision
	}
	return portalapi.PortalPreference{}, errors.New("PortalPreference 写入重试状态无效")
}

func (s *preferenceStore) preparePreferenceMutation(principal portalapi.Principal, request portalapi.PutPortalPreferenceRequest) (preferenceMutation, error) {
	key := preferenceRecordKey(principal, request.Scope)
	current, exists := s.state.Records[key]
	currentRevision := uint64(0)
	currentValues := emptyPreferenceValues()
	if exists {
		if current.TenantID != principal.TenantID || current.SubjectID != principal.ID {
			return preferenceMutation{}, errors.New("PortalPreference 记录身份绑定无效")
		}
		currentRevision, currentValues = current.Value.Revision, current.Value.Values
	}
	sections := portalapi.PortalPreferenceChangedSections(currentValues, request.Values)
	if request.ExpectedRevision != currentRevision {
		if exists && len(sections) == 0 {
			return preferenceMutation{value: clonePortalPreference(current.Value)}, nil
		}
		return preferenceMutation{}, portalapi.ErrPreferenceConflict
	}
	if !exists && len(s.state.Records) >= maximumPreferenceScopesPerUser {
		return preferenceMutation{}, errors.New("PortalPreference 用户 scope 数超过上限")
	}
	if exists && len(sections) == 0 {
		return preferenceMutation{value: clonePortalPreference(current.Value)}, nil
	}

	updated := portalapi.PortalPreference{
		Revision:  currentRevision + 1,
		Scope:     request.Scope,
		Values:    clonePreferenceValues(request.Values),
		UpdatedAt: s.now().UTC().Format(time.RFC3339Nano),
	}
	next := clonePreferenceState(s.state)
	next.Records[key] = storedPortalPreference{TenantID: principal.TenantID, SubjectID: principal.ID, Value: updated}
	next.NextAudit++
	next.Audit = append(next.Audit, preferenceAuditEvent{
		ID:        next.NextAudit,
		TenantID:  principal.TenantID,
		SubjectID: principal.ID,
		PortalID:  request.Scope.PortalID,
		Revision:  updated.Revision,
		Sections:  append([]string(nil), sections...),
		UpdatedAt: updated.UpdatedAt,
	})
	if len(next.Audit) > maximumPreferenceAuditEvents {
		next.Audit = append([]preferenceAuditEvent(nil), next.Audit[len(next.Audit)-maximumPreferenceAuditEvents:]...)
	}
	return preferenceMutation{value: updated, next: next, write: true}, nil
}
