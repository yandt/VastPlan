package portalcomposer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

const (
	PreferenceStateFileConfigKey   = "platform.portal-composer.preferenceStateFile"
	preferenceStateVersion         = 1
	maximumPreferenceStateBytes    = 16 << 20
	maximumPreferenceRecords       = 10000
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
	mu    sync.Mutex
	path  string
	state preferenceState
	now   func() time.Time
}

func openPreferenceStore(path string) (*preferenceStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("PortalPreference stateFile 不能为空")
	}
	store := &preferenceStore{
		path:  path,
		state: preferenceState{Version: preferenceStateVersion, Records: map[string]storedPortalPreference{}},
		now:   time.Now,
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *preferenceStore) Get(principal portalapi.Principal, scope portalapi.PortalPreferenceScope) (portalapi.PortalPreference, error) {
	if err := validatePreferencePrincipal(principal); err != nil {
		return portalapi.PortalPreference{}, err
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
		currentRevision = current.Value.Revision
		currentValues = current.Value.Values
	}
	if request.ExpectedRevision != currentRevision {
		if exists && len(portalapi.PortalPreferenceChangedSections(currentValues, request.Values)) == 0 {
			return clonePortalPreference(current.Value), nil
		}
		return portalapi.PortalPreference{}, portalapi.ErrPreferenceConflict
	}
	if !exists && s.userScopeCountLocked(principal) >= maximumPreferenceScopesPerUser {
		return portalapi.PortalPreference{}, errors.New("PortalPreference 用户 scope 数超过上限")
	}
	if !exists && len(s.state.Records) >= maximumPreferenceRecords {
		return portalapi.PortalPreference{}, errors.New("PortalPreference 记录总数超过上限")
	}
	sections := portalapi.PortalPreferenceChangedSections(currentValues, request.Values)
	if exists && len(sections) == 0 {
		return clonePortalPreference(current.Value), nil
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
		ID: next.NextAudit, TenantID: principal.TenantID, SubjectID: principal.ID,
		PortalID: request.Scope.PortalID, Revision: updated.Revision,
		Sections: append([]string(nil), sections...), UpdatedAt: updated.UpdatedAt,
	})
	if len(next.Audit) > maximumPreferenceAuditEvents {
		next.Audit = append([]preferenceAuditEvent(nil), next.Audit[len(next.Audit)-maximumPreferenceAuditEvents:]...)
	}
	if err := writePreferenceState(s.path, next); err != nil {
		return portalapi.PortalPreference{}, err
	}
	s.state = next
	return clonePortalPreference(updated), nil
}

func (s *preferenceStore) userScopeCountLocked(principal portalapi.Principal) int {
	count := 0
	for _, record := range s.state.Records {
		if record.TenantID == principal.TenantID && record.SubjectID == principal.ID {
			count++
		}
	}
	return count
}

func (s *preferenceStore) load() error {
	info, err := os.Lstat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return ensurePreferenceDirectory(filepath.Dir(s.path))
	}
	if err != nil {
		return fmt.Errorf("读取 PortalPreference 状态: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || info.Size() > maximumPreferenceStateBytes {
		return errors.New("PortalPreference 状态文件权限、类型或大小无效")
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("读取 PortalPreference 状态: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var state preferenceState
	if err := decoder.Decode(&state); err != nil {
		return fmt.Errorf("解析 PortalPreference 状态: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return err
	}
	if err := validatePreferenceState(state); err != nil {
		return err
	}
	s.state = state
	return nil
}

func validatePreferenceState(state preferenceState) error {
	if state.Version != preferenceStateVersion || state.Records == nil || len(state.Records) > maximumPreferenceRecords || len(state.Audit) > maximumPreferenceAuditEvents {
		return errors.New("PortalPreference 状态版本或容量无效")
	}
	for key, record := range state.Records {
		principal := portalapi.Principal{ID: record.SubjectID, TenantID: record.TenantID}
		if err := validatePreferencePrincipal(principal); err != nil || key != preferenceRecordKey(principal, record.Value.Scope) || record.Value.Revision == 0 {
			return errors.New("PortalPreference 状态记录身份、key 或 revision 无效")
		}
		if err := portalapi.ValidatePortalPreferenceScope(record.Value.Scope); err != nil {
			return err
		}
		if err := portalapi.ValidatePortalPreferenceValues(record.Value.Values); err != nil {
			return err
		}
	}
	return nil
}

func writePreferenceState(path string, state preferenceState) error {
	if err := ensurePreferenceDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if len(raw) > maximumPreferenceStateBytes {
		return errors.New("PortalPreference 状态超过大小上限")
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".portal-preferences-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := errors.Join(temporary.Sync(), temporary.Close()); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

func ensurePreferenceDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return errors.New("PortalPreference 状态目录必须是不可被组或其他用户写入的真实目录")
	}
	return nil
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
	out := portalapi.PortalPreferenceValues{
		RendererID:      value.RendererID,
		ShellTemplateID: value.ShellTemplateID,
	}
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

func clonePreferenceState(state preferenceState) preferenceState {
	out := preferenceState{Version: state.Version, NextAudit: state.NextAudit, Records: make(map[string]storedPortalPreference, len(state.Records))}
	for key, record := range state.Records {
		record.Value = clonePortalPreference(record.Value)
		out.Records[key] = record
	}
	out.Audit = make([]preferenceAuditEvent, len(state.Audit))
	for index, event := range state.Audit {
		event.Sections = append([]string(nil), event.Sections...)
		out.Audit[index] = event
	}
	return out
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("PortalPreference 状态包含多余 JSON")
	}
	return nil
}
