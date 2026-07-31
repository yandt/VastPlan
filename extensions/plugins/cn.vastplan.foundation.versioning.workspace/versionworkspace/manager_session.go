package versionworkspace

import (
	"context"
	"errors"
	"fmt"
	"time"

	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

func (m *Manager) ReadSnapshot(scope Scope, request workspacev1.SessionRequest) (workspacev1.SnapshotResult, error) {
	if m == nil || scope.Validate() != nil {
		return workspacev1.SnapshotResult{}, workspaceError(workspacev1.ErrorInvalidRequest, false, errors.New("读取 Version Workspace 请求无效"))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.activeRecordLocked(scope, request.SessionID, 0)
	if err != nil {
		return workspacev1.SnapshotResult{}, err
	}
	return workspacev1.SnapshotResult{Session: cloneSession(record.session), Snapshot: cloneSnapshot(record.current), Digest: record.currentDigest}, nil
}

func (m *Manager) WriteSnapshot(ctx context.Context, scope Scope, request workspacev1.WriteSnapshotRequest) (workspacev1.Session, error) {
	if m == nil || scope.Validate() != nil {
		return workspacev1.Session{}, workspaceError(workspacev1.ErrorInvalidRequest, false, errors.New("写入 Version Workspace 请求无效"))
	}
	m.mu.Lock()
	record, err := m.activeRecordLocked(scope, request.SessionID, request.ExpectedRevision)
	if err != nil {
		m.mu.Unlock()
		return workspacev1.Session{}, err
	}
	if record.session.ReadOnly {
		m.mu.Unlock()
		return workspacev1.Session{}, workspaceError(workspacev1.ErrorReadOnly, false, errors.New("只读 Version Workspace 不允许写入"))
	}
	if err := m.validateManifestContentLocked(record, request.ExpectedRevision, request.Snapshot); err != nil {
		m.mu.Unlock()
		return workspacev1.Session{}, err
	}
	session := cloneSession(record.session)
	binding, adapter, maxBytes := cloneBinding(record.binding), record.adapter, record.maxBytes
	m.mu.Unlock()

	normalized, err := adapter.Normalize(ctx, resourcev1.AdapterNormalizeRequest{
		Resource: session.Resource, Mode: session.Mode, Configuration: binding.AdapterConfig, Snapshot: cloneSnapshot(request.Snapshot),
	})
	if err != nil {
		return workspacev1.Session{}, workspaceError(workspacev1.ErrorAdapterUnavailable, false, err)
	}
	if err := resourcev1.ValidateAdapterNormalizeResult(normalized, maxBytes); err != nil {
		return workspacev1.Session{}, workspaceError(workspacev1.ErrorAdapterUnavailable, false, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	record, err = m.activeRecordLocked(scope, request.SessionID, request.ExpectedRevision)
	if err != nil {
		return workspacev1.Session{}, err
	}
	record.current = cloneSnapshot(normalized.Snapshot)
	record.currentDigest = normalized.Digest
	if record.currentDigest == record.baseDigest {
		record.session.State = workspacev1.StateClean
	} else {
		record.session.State = workspacev1.StateDirty
	}
	record.session.Revision++
	return cloneSession(record.session), nil
}

func (m *Manager) Changes(ctx context.Context, scope Scope, request workspacev1.SessionRequest) (workspacev1.ChangesResult, error) {
	if m == nil || scope.Validate() != nil {
		return workspacev1.ChangesResult{}, workspaceError(workspacev1.ErrorInvalidRequest, false, errors.New("读取 Version Workspace 变化请求无效"))
	}
	m.mu.Lock()
	record, err := m.activeRecordLocked(scope, request.SessionID, 0)
	if err != nil {
		m.mu.Unlock()
		return workspacev1.ChangesResult{}, err
	}
	session := cloneSession(record.session)
	base, current, adapter := cloneSnapshot(record.base), cloneSnapshot(record.current), record.adapter
	dirty := record.baseDigest != record.currentDigest
	m.mu.Unlock()

	diff, err := calculateDiff(ctx, adapter, resourcev1.AdapterDiffRequest{Resource: session.Resource, Mode: session.Mode, Left: base, Right: current}, dirty)
	if err != nil {
		return workspacev1.ChangesResult{}, err
	}
	return workspacev1.ChangesResult{Session: session, Dirty: dirty, DiffAvailable: diff.available, ChangedPaths: diff.paths, Summary: diff.summary}, nil
}

func (m *Manager) Renew(scope Scope, request workspacev1.RenewRequest) (workspacev1.Session, error) {
	if m == nil || scope.Validate() != nil {
		return workspacev1.Session{}, workspaceError(workspacev1.ErrorInvalidRequest, false, errors.New("续租 Version Workspace 请求无效"))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.activeRecordLocked(scope, request.SessionID, request.ExpectedRevision)
	if err != nil {
		return workspacev1.Session{}, err
	}
	if request.LeaseSeconds < 30 || request.LeaseSeconds > record.maxLease {
		return workspacev1.Session{}, workspaceError(workspacev1.ErrorLimitExceeded, false, fmt.Errorf("续租秒数必须在 30..%d", record.maxLease))
	}
	record.session.LeaseExpiresAt = m.now().UTC().Add(time.Duration(request.LeaseSeconds) * time.Second)
	record.session.Revision++
	return cloneSession(record.session), nil
}

func (m *Manager) Discard(scope Scope, request workspacev1.RevisionRequest) (workspacev1.Session, error) {
	if m == nil || scope.Validate() != nil {
		return workspacev1.Session{}, workspaceError(workspacev1.ErrorInvalidRequest, false, errors.New("丢弃 Version Workspace 请求无效"))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.activeRecordLocked(scope, request.SessionID, request.ExpectedRevision)
	if err != nil {
		return workspacev1.Session{}, err
	}
	record.session.State = workspacev1.StateDiscarded
	record.session.Revision++
	record.current = resourcev1.Snapshot{}
	record.currentDigest = ""
	return cloneSession(record.session), nil
}
