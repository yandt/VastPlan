package portalcomposer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	sharedstatesdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/sharedstate"
)

const (
	preferenceStateNamespace       = "portal.preferences"
	preferenceStateVersion         = 1
	maximumPreferenceStateBytes    = 1 << 20
	maximumPreferenceScopesPerUser = 64
	maximumPreferenceAuditEvents   = 2048
)

type preferenceAuditEvent struct {
	ID        uint64   `json:"id"`
	TenantID  string   `json:"tenantId"`
	SubjectID string   `json:"subjectId"`
	PortalID  string   `json:"portalId"`
	Revision  uint64   `json:"revision"`
	Sections  []string `json:"sections"`
	UpdatedAt string   `json:"updatedAt"`
}

type storedPortalPreference struct {
	TenantID  string                     `json:"tenantId"`
	SubjectID string                     `json:"subjectId"`
	Value     portalapi.PortalPreference `json:"value"`
}

type preferenceState struct {
	Version   int                               `json:"version"`
	NextAudit uint64                            `json:"nextAudit"`
	Records   map[string]storedPortalPreference `json:"records"`
	Audit     []preferenceAuditEvent            `json:"audit,omitempty"`
}

type preferenceStore struct {
	mu        sync.Mutex
	state     preferenceState
	revision  uint64
	principal portalapi.Principal
	persist   func(preferenceState, uint64) (uint64, error)
	now       func() time.Time
}

func newPreferenceStore(ctx context.Context, host sdk.Host, call *contractv1.CallContext, principal portalapi.Principal) (*preferenceStore, error) {
	if err := validatePreferencePrincipal(principal); err != nil || call == nil || call.GetTenantId() != principal.TenantID {
		return nil, portalapi.ErrForbidden
	}
	client, err := sharedstatesdk.New(host, "tenant", preferenceStateNamespace)
	if err != nil {
		return nil, err
	}
	key := preferenceDocumentKey(principal.ID)
	value := emptyPreferenceState()
	revision := uint64(0)
	entry, err := client.Get(ctx, call, key)
	if err == nil {
		if err := decodePreferenceState(entry.Value, &value); err != nil {
			return nil, err
		}
		revision = entry.Revision
	} else if !sharedstatesdk.IsNotFound(err) {
		return nil, fmt.Errorf("读取 PortalPreference Shared State: %w", err)
	}
	if err := validatePreferenceStateForPrincipal(value, principal); err != nil {
		return nil, err
	}
	store := &preferenceStore{state: value, revision: revision, principal: principal, now: time.Now}
	store.persist = func(next preferenceState, expected uint64) (uint64, error) {
		raw, err := json.Marshal(next)
		if err != nil {
			return 0, err
		}
		if len(raw) > maximumPreferenceStateBytes {
			return 0, errors.New("PortalPreference 用户文档超过 Shared State 单值上限")
		}
		var entry sharedstatesdk.Entry
		if expected == 0 {
			entry, err = client.Create(ctx, call, key, raw)
		} else {
			entry, err = client.Update(ctx, call, key, raw, expected)
		}
		if sharedstatesdk.IsConflict(err) {
			return 0, portalapi.ErrPreferenceConflict
		}
		if err != nil {
			return 0, fmt.Errorf("保存 PortalPreference Shared State: %w", err)
		}
		return entry.Revision, nil
	}
	return store, nil
}

func emptyPreferenceState() preferenceState {
	return preferenceState{Version: preferenceStateVersion, Records: map[string]storedPortalPreference{}}
}

func (s *preferenceStore) Get(principal portalapi.Principal, scope portalapi.PortalPreferenceScope) (portalapi.PortalPreference, error) {
	if err := validatePreferencePrincipal(principal); err != nil {
		return portalapi.PortalPreference{}, err
	}
	if s.principal.ID != "" && (s.principal.ID != principal.ID || s.principal.TenantID != principal.TenantID) {
		return portalapi.PortalPreference{}, portalapi.ErrForbidden
	}
	if err := portalapi.ValidatePortalPreferenceScope(scope); err != nil {
		return portalapi.PortalPreference{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.state.Records[preferenceRecordKey(principal, scope)]
	if !ok {
		return portalapi.PortalPreference{Scope: scope, Values: emptyPreferenceValues()}, nil
	}
	if record.TenantID != principal.TenantID || record.SubjectID != principal.ID {
		return portalapi.PortalPreference{}, errors.New("PortalPreference 记录身份绑定无效")
	}
	return clonePortalPreference(record.Value), nil
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
	key := preferenceRecordKey(principal, request.Scope)
	current, exists := s.state.Records[key]
	currentRevision := uint64(0)
	currentValues := emptyPreferenceValues()
	if exists {
		if current.TenantID != principal.TenantID || current.SubjectID != principal.ID {
			return portalapi.PortalPreference{}, errors.New("PortalPreference 记录身份绑定无效")
		}
		currentRevision, currentValues = current.Value.Revision, current.Value.Values
	}
	if request.ExpectedRevision != currentRevision {
		if exists && len(portalapi.PortalPreferenceChangedSections(currentValues, request.Values)) == 0 {
			return clonePortalPreference(current.Value), nil
		}
		return portalapi.PortalPreference{}, portalapi.ErrPreferenceConflict
	}
	if !exists && len(s.state.Records) >= maximumPreferenceScopesPerUser {
		return portalapi.PortalPreference{}, errors.New("PortalPreference 用户 scope 数超过上限")
	}
	sections := portalapi.PortalPreferenceChangedSections(currentValues, request.Values)
	if exists && len(sections) == 0 {
		return clonePortalPreference(current.Value), nil
	}
	updated := portalapi.PortalPreference{Revision: currentRevision + 1, Scope: request.Scope, Values: clonePreferenceValues(request.Values), UpdatedAt: s.now().UTC().Format(time.RFC3339Nano)}
	next := clonePreferenceState(s.state)
	next.Records[key] = storedPortalPreference{TenantID: principal.TenantID, SubjectID: principal.ID, Value: updated}
	next.NextAudit++
	next.Audit = append(next.Audit, preferenceAuditEvent{ID: next.NextAudit, TenantID: principal.TenantID, SubjectID: principal.ID, PortalID: request.Scope.PortalID, Revision: updated.Revision, Sections: append([]string(nil), sections...), UpdatedAt: updated.UpdatedAt})
	if len(next.Audit) > maximumPreferenceAuditEvents {
		next.Audit = append([]preferenceAuditEvent(nil), next.Audit[len(next.Audit)-maximumPreferenceAuditEvents:]...)
	}
	if s.persist == nil {
		return portalapi.PortalPreference{}, errors.New("PortalPreference 写入缺少 Shared State 会话")
	}
	revision, err := s.persist(next, s.revision)
	if err != nil {
		return portalapi.PortalPreference{}, err
	}
	s.state, s.revision = next, revision
	return clonePortalPreference(updated), nil
}

func decodePreferenceState(raw []byte, target *preferenceState) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("解析 PortalPreference Shared State: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("PortalPreference Shared State 包含尾随数据")
	}
	return nil
}

func validatePreferenceStateForPrincipal(value preferenceState, principal portalapi.Principal) error {
	if value.Version != preferenceStateVersion || value.Records == nil || len(value.Records) > maximumPreferenceScopesPerUser || len(value.Audit) > maximumPreferenceAuditEvents {
		return errors.New("PortalPreference 状态版本或容量无效")
	}
	for key, record := range value.Records {
		if record.TenantID != principal.TenantID || record.SubjectID != principal.ID || key != preferenceRecordKey(principal, record.Value.Scope) || record.Value.Revision == 0 {
			return errors.New("PortalPreference 状态记录身份、key 或 revision 无效")
		}
		if err := portalapi.ValidatePortalPreferenceScope(record.Value.Scope); err != nil {
			return err
		}
		if err := portalapi.ValidatePortalPreferenceValues(record.Value.Values); err != nil {
			return err
		}
	}
	for _, event := range value.Audit {
		if event.TenantID != principal.TenantID || event.SubjectID != principal.ID {
			return errors.New("PortalPreference 审计身份绑定无效")
		}
	}
	return nil
}

func preferenceDocumentKey(subject string) string {
	digest := sha256.Sum256([]byte(subject))
	return "subject-" + hex.EncodeToString(digest[:])
}

func preferenceRecordKey(principal portalapi.Principal, scope portalapi.PortalPreferenceScope) string {
	digest := sha256.Sum256([]byte(principal.TenantID + "\x00" + principal.ID + "\x00" + portalapi.PortalPreferenceScopeKey(scope)))
	return hex.EncodeToString(digest[:])
}

func validatePreferencePrincipal(principal portalapi.Principal) error {
	if invalidPreferenceIdentity(principal.TenantID) || invalidPreferenceIdentity(principal.ID) {
		return portalapi.ErrForbidden
	}
	return nil
}

func invalidPreferenceIdentity(value string) bool {
	return strings.TrimSpace(value) == "" || len(value) > 256 || strings.ContainsRune(value, '\x00')
}

func clonePortalPreference(value portalapi.PortalPreference) portalapi.PortalPreference {
	value.Values = clonePreferenceValues(value.Values)
	return value
}

func clonePreferenceValues(value portalapi.PortalPreferenceValues) portalapi.PortalPreferenceValues {
	out := portalapi.PortalPreferenceValues{RendererID: value.RendererID, ShellTemplateID: value.ShellTemplateID}
	if len(value.RendererOptions) > 0 {
		out.RendererOptions = make(map[string]portalapi.RendererPreference, len(value.RendererOptions))
		for key, option := range value.RendererOptions {
			out.RendererOptions[key] = option
		}
	}
	if len(value.Collections) > 0 {
		out.Collections = make(map[string]portalapi.CollectionPreference, len(value.Collections))
		for key, preference := range value.Collections {
			preference.Columns = append([]string(nil), preference.Columns...)
			preference.HiddenColumns = append([]string(nil), preference.HiddenColumns...)
			out.Collections[key] = preference
		}
	}
	return out
}

func emptyPreferenceValues() portalapi.PortalPreferenceValues {
	return portalapi.PortalPreferenceValues{}
}

func clonePreferenceState(value preferenceState) preferenceState {
	out := preferenceState{Version: value.Version, NextAudit: value.NextAudit, Records: make(map[string]storedPortalPreference, len(value.Records))}
	for key, record := range value.Records {
		record.Value = clonePortalPreference(record.Value)
		out.Records[key] = record
	}
	out.Audit = make([]preferenceAuditEvent, len(value.Audit))
	for index, event := range value.Audit {
		event.Sections = append([]string(nil), event.Sections...)
		out.Audit[index] = event
	}
	return out
}
