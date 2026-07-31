package versionworkspace

import (
	"context"
	"errors"

	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

func (m *Manager) BeginContentUpload(ctx context.Context, scope Scope, staging Staging, request workspacev1.BeginContentUploadRequest) (workspacev1.ContentUploadResult, error) {
	if m == nil || staging == nil || scope.Validate() != nil || workspacev1.ValidateBeginContentUploadRequest(request) != nil {
		return workspacev1.ContentUploadResult{}, workspaceError(workspacev1.ErrorInvalidRequest, false, errors.New("开始 Workspace 内容上传请求无效"))
	}
	m.mu.Lock()
	record, err := m.activeRecordLocked(scope, request.SessionID, request.ExpectedRevision)
	if err != nil {
		m.mu.Unlock()
		return workspacev1.ContentUploadResult{}, err
	}
	if record.session.ReadOnly {
		m.mu.Unlock()
		return workspacev1.ContentUploadResult{}, workspaceError(workspacev1.ErrorReadOnly, false, errors.New("只读 Workspace 不允许上传内容"))
	}
	if record.adapter.Descriptor().ContentKind != resourcev1.ContentFiles {
		m.mu.Unlock()
		return workspacev1.ContentUploadResult{}, workspaceError(workspacev1.ErrorOperationUnsupported, false, errors.New("当前 Resource Adapter 不使用暂存内容"))
	}
	if request.ExpectedSize > record.maxBytes || request.LeaseSeconds > record.maxLease {
		m.mu.Unlock()
		return workspacev1.ContentUploadResult{}, workspaceError(workspacev1.ErrorLimitExceeded, false, errors.New("内容上传超过 Environment 限制"))
	}
	session := cloneSession(record.session)
	m.mu.Unlock()

	status, err := staging.BeginUpload(ctx, stagingv1.BeginUploadRequest{
		SessionID: session.ID, ExpectedSessionRevision: request.ExpectedRevision, EnvironmentDigest: session.EnvironmentDigest,
		Resource: session.Resource, Path: request.Path, MediaType: request.MediaType, ExpectedDigest: request.ExpectedDigest,
		ExpectedSize: request.ExpectedSize, LeaseSeconds: request.LeaseSeconds,
	})
	if err != nil {
		return workspacev1.ContentUploadResult{}, mapStagingFailure(err)
	}
	if err := validateStagingBinding(session, request, status); err != nil {
		return workspacev1.ContentUploadResult{}, workspaceError(workspacev1.ErrorContentUnavailable, false, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	record, err = m.activeRecordLocked(scope, request.SessionID, request.ExpectedRevision)
	if err != nil {
		return workspacev1.ContentUploadResult{}, err
	}
	if existing, ok := record.uploads[status.Upload.ID]; ok && !sameStagedContent(existing.status, status) {
		return workspacev1.ContentUploadResult{}, workspaceError(workspacev1.ErrorContentUnavailable, false, errors.New("Upload ID 已绑定其他内容声明"))
	}
	record.uploads[status.Upload.ID] = stagedContent{sessionRevision: request.ExpectedRevision, status: status}
	return workspacev1.ContentUploadResult{Session: cloneSession(record.session), Upload: status}, nil
}

func (m *Manager) ContentUploadStatus(ctx context.Context, scope Scope, staging Staging, request workspacev1.ContentUploadRequest) (workspacev1.ContentUploadResult, error) {
	return m.updateContentUpload(scope, staging, request.SessionID, request.UploadID, func() (stagingv1.UploadStatusResult, error) {
		return staging.UploadStatus(ctx, request.UploadID)
	})
}

func (m *Manager) RenewContentUpload(ctx context.Context, scope Scope, staging Staging, request workspacev1.RenewContentUploadRequest) (workspacev1.ContentUploadResult, error) {
	if workspacev1.ValidateRenewContentUploadRequest(request) != nil {
		return workspacev1.ContentUploadResult{}, workspaceError(workspacev1.ErrorInvalidRequest, false, errors.New("续租 Workspace 内容上传请求无效"))
	}
	return m.updateContentUpload(scope, staging, request.SessionID, request.UploadID, func() (stagingv1.UploadStatusResult, error) {
		return staging.RenewUpload(ctx, stagingv1.RenewUploadRequest{UploadID: request.UploadID, ExpectedRevision: request.ExpectedUploadRevision, LeaseSeconds: request.LeaseSeconds})
	})
}

func (m *Manager) CompleteContentUpload(ctx context.Context, scope Scope, staging Staging, request workspacev1.ContentUploadRevisionRequest) (workspacev1.ContentUploadResult, error) {
	if workspacev1.ValidateContentUploadRevisionRequest(request) != nil {
		return workspacev1.ContentUploadResult{}, workspaceError(workspacev1.ErrorInvalidRequest, false, errors.New("完成 Workspace 内容上传请求无效"))
	}
	return m.updateContentUpload(scope, staging, request.SessionID, request.UploadID, func() (stagingv1.UploadStatusResult, error) {
		return staging.CompleteUpload(ctx, stagingv1.UploadRevisionRequest{UploadID: request.UploadID, ExpectedRevision: request.ExpectedUploadRevision})
	})
}

func (m *Manager) AbortContentUpload(ctx context.Context, scope Scope, staging Staging, request workspacev1.ContentUploadRevisionRequest) (workspacev1.ContentUploadResult, error) {
	if workspacev1.ValidateContentUploadRevisionRequest(request) != nil {
		return workspacev1.ContentUploadResult{}, workspaceError(workspacev1.ErrorInvalidRequest, false, errors.New("终止 Workspace 内容上传请求无效"))
	}
	return m.updateContentUpload(scope, staging, request.SessionID, request.UploadID, func() (stagingv1.UploadStatusResult, error) {
		return staging.AbortUpload(ctx, stagingv1.UploadRevisionRequest{UploadID: request.UploadID, ExpectedRevision: request.ExpectedUploadRevision})
	})
}

func (m *Manager) updateContentUpload(scope Scope, staging Staging, sessionID, uploadID string, call func() (stagingv1.UploadStatusResult, error)) (workspacev1.ContentUploadResult, error) {
	if m == nil || staging == nil || scope.Validate() != nil || workspacev1.ValidateContentUploadRequest(workspacev1.ContentUploadRequest{SessionID: sessionID, UploadID: uploadID}) != nil {
		return workspacev1.ContentUploadResult{}, workspaceError(workspacev1.ErrorInvalidRequest, false, errors.New("Workspace 内容上传请求无效"))
	}
	m.mu.Lock()
	record, err := m.activeRecordLocked(scope, sessionID, 0)
	if err != nil {
		m.mu.Unlock()
		return workspacev1.ContentUploadResult{}, err
	}
	binding, exists := record.uploads[uploadID]
	m.mu.Unlock()
	if !exists {
		return workspacev1.ContentUploadResult{}, workspaceError(workspacev1.ErrorContentUnavailable, false, errors.New("Workspace Session 未绑定该 Upload"))
	}
	status, err := call()
	if err != nil {
		return workspacev1.ContentUploadResult{}, mapStagingFailure(err)
	}
	if stagingv1.ValidateUploadStatusResult(status) != nil || !sameStagedContent(binding.status, status) {
		return workspacev1.ContentUploadResult{}, workspaceError(workspacev1.ErrorContentUnavailable, false, errors.New("Content Staging 返回了不同内容身份"))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err = m.activeRecordLocked(scope, sessionID, 0)
	if err != nil {
		return workspacev1.ContentUploadResult{}, err
	}
	current, exists := record.uploads[uploadID]
	if !exists || current.sessionRevision != binding.sessionRevision || current.status.Upload.Revision != binding.status.Upload.Revision || !sameStagedContent(current.status, binding.status) {
		return workspacev1.ContentUploadResult{}, workspaceError(workspacev1.ErrorSessionConflict, false, errors.New("Workspace 内容绑定已变化"))
	}
	current.status = status
	record.uploads[uploadID] = current
	return workspacev1.ContentUploadResult{Session: cloneSession(record.session), Upload: status}, nil
}

func validateStagingBinding(session workspacev1.Session, request workspacev1.BeginContentUploadRequest, status stagingv1.UploadStatusResult) error {
	if stagingv1.ValidateUploadStatusResult(status) != nil {
		return errors.New("Content Staging 返回无效状态")
	}
	upload := status.Upload
	if upload.SessionID != session.ID || upload.EnvironmentDigest != session.EnvironmentDigest || upload.Resource != session.Resource || upload.Path != request.Path ||
		upload.MediaType != request.MediaType || upload.ExpectedDigest != request.ExpectedDigest || upload.ExpectedSize != request.ExpectedSize {
		return errors.New("Content Staging 状态与 Workspace 声明不匹配")
	}
	return nil
}

func sameStagedContent(left, right stagingv1.UploadStatusResult) bool {
	return left.Upload.ID == right.Upload.ID && left.Upload.SessionID == right.Upload.SessionID && left.Upload.EnvironmentDigest == right.Upload.EnvironmentDigest &&
		left.Upload.Resource == right.Upload.Resource && left.Upload.Path == right.Upload.Path && left.Upload.MediaType == right.Upload.MediaType &&
		left.Upload.ExpectedDigest == right.Upload.ExpectedDigest && left.Upload.ExpectedSize == right.Upload.ExpectedSize
}

func (m *Manager) validateManifestContentLocked(record *sessionRecord, revision uint64, snapshot resourcev1.Snapshot) error {
	if snapshot.Kind != resourcev1.ContentFiles {
		return nil
	}
	base := make(map[string]resourcev1.FileEntry, len(record.base.Files))
	for _, entry := range record.base.Files {
		base[entry.Path] = entry
	}
	current := make(map[string]resourcev1.FileEntry, len(record.current.Files))
	for _, entry := range record.current.Files {
		current[entry.Path] = entry
	}
	for _, entry := range snapshot.Files {
		if existing, ok := base[entry.Path]; ok && sameContentEntry(existing, entry) {
			continue
		}
		unchangedCandidate := false
		if existing, ok := current[entry.Path]; ok && sameContentEntry(existing, entry) {
			unchangedCandidate = true
		}
		ready := false
		for _, binding := range record.uploads {
			if (binding.sessionRevision == revision || unchangedCandidate) && binding.status.Upload.State == stagingv1.StateReady && m.now().Before(binding.status.Upload.LeaseExpiresAt) && binding.status.Content != nil &&
				binding.status.Upload.Path == entry.Path && binding.status.Content.Digest == entry.Digest && binding.status.Content.Size == entry.Size && binding.status.Content.MediaType == entry.MediaType {
				ready = true
				break
			}
		}
		if !ready {
			return workspaceError(workspacev1.ErrorContentUnavailable, false, errors.New("Files Manifest 引用了未在当前 Session revision 就绪的内容"))
		}
	}
	return nil
}

func sameContentEntry(left, right resourcev1.FileEntry) bool {
	return left.Path == right.Path && left.Digest == right.Digest && left.Size == right.Size && left.MediaType == right.MediaType
}
