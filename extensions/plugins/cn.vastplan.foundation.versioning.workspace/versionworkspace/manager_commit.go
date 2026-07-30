package versionworkspace

import (
	"context"
	"errors"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

func (m *Manager) Commit(ctx context.Context, scope Scope, ledger Ledger, request workspacev1.CommitRequest) (workspacev1.CommitResult, error) {
	if m == nil || ledger == nil || scope.Validate() != nil || workspacev1.ValidateCommitRequest(request) != nil {
		return workspacev1.CommitResult{}, workspaceError(workspacev1.ErrorInvalidRequest, false, errors.New("提交 Version Workspace 请求无效"))
	}
	request.Labels = cloneLabels(request.Labels)
	m.mu.Lock()
	if existing, ok := m.committedRetryLocked(scope, request); ok {
		m.mu.Unlock()
		return existing, nil
	}
	record, err := m.activeRecordLocked(scope, request.SessionID, request.ExpectedRevision)
	if err != nil {
		m.mu.Unlock()
		return workspacev1.CommitResult{}, err
	}
	if record.session.ReadOnly {
		m.mu.Unlock()
		return workspacev1.CommitResult{}, workspaceError(workspacev1.ErrorReadOnly, false, errors.New("只读 Version Workspace 不允许提交"))
	}
	record.preCommitState = record.session.State
	record.session.State = workspacev1.StateCommitting
	session := cloneSession(record.session)
	snapshot := cloneSnapshot(record.current)
	maxBytes := record.maxBytes
	m.mu.Unlock()

	content, err := ledgerContent(snapshot, maxBytes)
	if err != nil {
		m.restoreCommitState(scope, request.SessionID, request.ExpectedRevision)
		return workspacev1.CommitResult{}, workspaceError(workspacev1.ErrorAdapterUnavailable, false, err)
	}
	parents := []versioningv1.VersionRef(nil)
	if session.BaseRef != nil {
		parents = append(parents, *session.BaseRef)
	}
	put, err := ledger.PutVersion(ctx, versioningv1.PutVersionRequest{
		Stream:         versioningv1.StreamKey{Namespace: session.Namespace, StreamID: session.Resource.ID},
		IdempotencyKey: commitIdempotencyKey(session.ID, request.ExpectedRevision), Parents: parents,
		Content: content, Message: request.Message, Labels: cloneLabels(request.Labels),
	})
	if err != nil {
		m.restoreCommitState(scope, request.SessionID, request.ExpectedRevision)
		return workspacev1.CommitResult{}, mapLedgerFailure(err, workspacev1.ErrorBaseConflict)
	}

	head, err := commitHead(ctx, ledger, session, put.Version.Ref)
	if err != nil {
		m.restoreCommitState(scope, request.SessionID, request.ExpectedRevision)
		return workspacev1.CommitResult{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	record, err = m.ownedRecordLocked(scope, request.SessionID)
	if err != nil || record.session.State != workspacev1.StateCommitting || record.session.Revision != request.ExpectedRevision {
		return workspacev1.CommitResult{}, workspaceError(workspacev1.ErrorSessionConflict, false, errors.New("Version Workspace 提交完成时 Session 状态已改变"))
	}
	record.session.State = workspacev1.StateCommitted
	record.session.Revision++
	if head != nil {
		record.session.HeadRevision = head.Revision
	}
	result := workspacev1.CommitResult{Session: cloneSession(record.session), Version: put.Version, Head: head}
	record.commitRequest = &workspacev1.CommitRequest{SessionID: request.SessionID, ExpectedRevision: request.ExpectedRevision, Message: request.Message, Labels: cloneLabels(request.Labels)}
	cloned := cloneCommitResult(result)
	record.commitResult = &cloned
	return cloneCommitResult(result), nil
}

func (m *Manager) committedRetryLocked(scope Scope, request workspacev1.CommitRequest) (workspacev1.CommitResult, bool) {
	record := m.sessions[request.SessionID]
	if record == nil || record.owner != scope || record.commitRequest == nil || record.commitResult == nil {
		return workspacev1.CommitResult{}, false
	}
	if record.commitRequest.SessionID != request.SessionID || record.commitRequest.ExpectedRevision != request.ExpectedRevision || record.commitRequest.Message != request.Message || !equalLabels(record.commitRequest.Labels, request.Labels) {
		return workspacev1.CommitResult{}, false
	}
	return cloneCommitResult(*record.commitResult), true
}

func (m *Manager) restoreCommitState(scope Scope, sessionID string, revision uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record := m.sessions[sessionID]
	if record != nil && record.owner == scope && record.session.State == workspacev1.StateCommitting && record.session.Revision == revision {
		record.session.State = record.preCommitState
		record.preCommitState = ""
	}
}

func commitHead(ctx context.Context, ledger Ledger, session workspacev1.Session, target versioningv1.VersionRef) (*versioningv1.Head, error) {
	if session.TargetHead == "" {
		return nil, nil
	}
	stream := versioningv1.StreamKey{Namespace: session.Namespace, StreamID: session.Resource.ID}
	var head versioningv1.Head
	var err error
	if session.HeadRevision == 0 {
		created, createErr := ledger.CreateHead(ctx, versioningv1.CreateHeadRequest{Stream: stream, Name: session.TargetHead, Target: target})
		head, err = created.Head, createErr
	} else {
		moved, moveErr := ledger.MoveHead(ctx, versioningv1.MoveHeadRequest{Stream: stream, Name: session.TargetHead, Target: target, ExpectedRevision: session.HeadRevision})
		head, err = moved.Head, moveErr
	}
	if err == nil {
		return &head, nil
	}
	// The write may have committed while its response was lost. Read-after-error
	// converts that ambiguous outcome into deterministic success or conflict.
	observed, readErr := ledger.GetHead(ctx, versioningv1.GetHeadRequest{Stream: stream, Name: session.TargetHead})
	if readErr == nil && observed.Head.Target == target {
		return &observed.Head, nil
	}
	if readErr == nil || isLedgerCode(err, versioningv1.ErrorConflict) {
		return nil, workspaceError(workspacev1.ErrorBaseConflict, false, errors.New("Version Head 已被其他编辑会话更新"))
	}
	return nil, mapLedgerFailure(err, workspacev1.ErrorBaseConflict)
}

func commitIdempotencyKey(sessionID string, revision uint64) string {
	return sessionID + ":commit:" + fmtUint(revision)
}

func fmtUint(value uint64) string {
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[index:])
}
