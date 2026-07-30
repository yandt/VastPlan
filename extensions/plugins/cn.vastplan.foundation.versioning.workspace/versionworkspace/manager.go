package versionworkspace

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

const (
	defaultLeaseSeconds = 900
	defaultRetention    = 5 * time.Minute
)

type ManagerOptions struct {
	Now              func() time.Time
	NewSessionID     func() (string, error)
	ExpiredRetention time.Duration
}

type sessionRecord struct {
	session        workspacev1.Session
	owner          Scope
	binding        resourcev1.ResourceBinding
	adapter        Adapter
	maxBytes       int64
	maxLease       int
	base           resourcev1.Snapshot
	baseDigest     string
	current        resourcev1.Snapshot
	currentDigest  string
	commitRequest  *workspacev1.CommitRequest
	commitResult   *workspacev1.CommitResult
	preCommitState string
}

type Manager struct {
	mu        sync.Mutex
	catalog   *Catalog
	sessions  map[string]*sessionRecord
	now       func() time.Time
	newID     func() (string, error)
	retention time.Duration
}

func NewManager(catalog *Catalog, options ManagerOptions) (*Manager, error) {
	if catalog == nil {
		return nil, errors.New("Version Workspace Catalog 不能为空")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.NewSessionID == nil {
		options.NewSessionID = secureSessionID
	}
	if options.ExpiredRetention <= 0 {
		options.ExpiredRetention = defaultRetention
	}
	return &Manager{catalog: catalog, sessions: map[string]*sessionRecord{}, now: options.Now, newID: options.NewSessionID, retention: options.ExpiredRetention}, nil
}

func secureSessionID() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "ws_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func (m *Manager) ownedRecordLocked(scope Scope, sessionID string) (*sessionRecord, error) {
	record := m.sessions[sessionID]
	if record == nil || record.owner != scope {
		return nil, workspaceError(workspacev1.ErrorSessionNotFound, false, errors.New("Version Workspace Session 不存在"))
	}
	return record, nil
}

func (m *Manager) activeRecordLocked(scope Scope, sessionID string, expectedRevision uint64) (*sessionRecord, error) {
	record, err := m.ownedRecordLocked(scope, sessionID)
	if err != nil {
		return nil, err
	}
	m.expireRecordLocked(record, m.now())
	if record.session.State == workspacev1.StateExpired {
		return nil, workspaceError(workspacev1.ErrorLeaseExpired, false, errors.New("Version Workspace Lease 已过期"))
	}
	if expectedRevision != 0 && record.session.Revision != expectedRevision {
		return nil, workspaceError(workspacev1.ErrorSessionConflict, false, fmt.Errorf("Version Workspace revision 冲突: expected=%d actual=%d", expectedRevision, record.session.Revision))
	}
	if record.session.State != workspacev1.StateClean && record.session.State != workspacev1.StateDirty {
		return nil, workspaceError(workspacev1.ErrorSessionConflict, false, fmt.Errorf("Version Workspace 当前状态 %s 不允许该操作", record.session.State))
	}
	return record, nil
}

func (m *Manager) expireRecordLocked(record *sessionRecord, now time.Time) {
	if (record.session.State == workspacev1.StateClean || record.session.State == workspacev1.StateDirty) && !now.Before(record.session.LeaseExpiresAt) {
		record.session.State = workspacev1.StateExpired
		record.session.Revision++
		record.current = resourcev1.Snapshot{}
	}
}

func (m *Manager) sweepLocked(now time.Time) int {
	removed := 0
	for id, record := range m.sessions {
		m.expireRecordLocked(record, now)
		if now.After(record.session.LeaseExpiresAt.Add(m.retention)) {
			delete(m.sessions, id)
			removed++
		}
	}
	return removed
}

func (m *Manager) SweepExpired() int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sweepLocked(m.now())
}

func (m *Manager) Status(scope Scope, request workspacev1.SessionRequest) (workspacev1.Session, error) {
	if err := scope.Validate(); err != nil {
		return workspacev1.Session{}, workspaceError(workspacev1.ErrorInvalidRequest, false, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.ownedRecordLocked(scope, request.SessionID)
	if err != nil {
		return workspacev1.Session{}, err
	}
	m.expireRecordLocked(record, m.now())
	return cloneSession(record.session), nil
}

func cloneSession(session workspacev1.Session) workspacev1.Session {
	if session.BaseRef != nil {
		ref := *session.BaseRef
		session.BaseRef = &ref
	}
	return session
}

func cloneSnapshot(snapshot resourcev1.Snapshot) resourcev1.Snapshot {
	snapshot.JSON = append(json.RawMessage(nil), snapshot.JSON...)
	snapshot.Files = append([]resourcev1.FileEntry(nil), snapshot.Files...)
	return snapshot
}

func cloneLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return nil
	}
	cloned := make(map[string]string, len(labels))
	for key, value := range labels {
		cloned[key] = value
	}
	return cloned
}

func equalLabels(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func cloneCommitResult(result workspacev1.CommitResult) workspacev1.CommitResult {
	result.Session = cloneSession(result.Session)
	result.Version.Content = append(json.RawMessage(nil), result.Version.Content...)
	result.Version.Parents = append([]versioningv1.VersionRef(nil), result.Version.Parents...)
	result.Version.Labels = cloneLabels(result.Version.Labels)
	if result.Head != nil {
		head := *result.Head
		result.Head = &head
	}
	return result
}
