package contentstaging

import (
	"context"
	"errors"
	"io"

	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
)

// Stream is the trusted Go data-plane port. It reads incrementally and never
// materializes the file in a Capability Bus payload or process memory.
func (m *Manager) Stream(ctx context.Context, scope Scope, uploadID string, reader io.Reader) (stagingv1.UploadStatusResult, error) {
	if err := scope.Validate(); err != nil || reader == nil {
		if err == nil {
			err = errors.New("上传数据流不能为空")
		}
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorInvalidRequest, false, err)
	}
	if err := stagingv1.ValidateUploadStatusRequest(stagingv1.UploadStatusRequest{UploadID: uploadID}); err != nil {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorInvalidRequest, false, err)
	}
	m.mu.Lock()
	record, err := m.ownedRecordLocked(scope, uploadID)
	if err != nil {
		m.mu.Unlock()
		return stagingv1.UploadStatusResult{}, err
	}
	if expired, err := m.expireIfNeededLocked(ctx, record); err != nil {
		m.mu.Unlock()
		return stagingv1.UploadStatusResult{}, err
	} else if expired {
		m.mu.Unlock()
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorLeaseExpired, false, errors.New("Content Staging Lease 已过期"))
	}
	if record.Upload.State != stagingv1.StatePending && record.Upload.State != stagingv1.StateUploading {
		m.mu.Unlock()
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorLeaseConflict, false, errors.New("当前 Upload 状态不接受数据流"))
	}
	if _, busy := m.writers[uploadID]; busy {
		m.mu.Unlock()
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorLeaseConflict, true, errors.New("Upload 已有活动写入流"))
	}
	before := cloneRecord(*record)
	record.Upload.State = stagingv1.StateUploading
	record.Upload.UpdatedAt = m.now()
	if err := m.saveOrRollbackLocked(ctx, record, before); err != nil {
		m.mu.Unlock()
		return stagingv1.UploadStatusResult{}, err
	}
	m.writers[uploadID] = struct{}{}
	declaredSize := record.Upload.ExpectedSize
	m.mu.Unlock()

	written, writeErr := m.provider.WriteStaged(ctx, scope, uploadID, declaredSize, reader)

	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.writers, uploadID)
	record, exists := m.uploads[uploadID]
	if !exists || record.Owner != scope {
		_ = m.provider.RemoveStaged(context.Background(), scope, uploadID)
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorLeaseNotFound, false, errors.New("Content Staging Lease 不存在"))
	}
	if record.Upload.State == stagingv1.StateAborted || record.Upload.State == stagingv1.StateExpired {
		_ = m.provider.RemoveStaged(context.Background(), scope, uploadID)
		return record.result(), stagingError(stagingv1.ErrorLeaseConflict, false, errors.New("数据流完成前 Upload 已终止"))
	}
	if errors.Is(writeErr, errStreamLimitExceeded) {
		return m.rejectLocked(ctx, record, stagingv1.FailureSizeMismatch)
	}
	if writeErr != nil {
		before = cloneRecord(*record)
		record.Written, record.ActualDigest, record.Upload.ReceivedSize = false, "", 0
		record.Upload.UpdatedAt = m.now()
		if err := m.saveOrRollbackLocked(ctx, record, before); err != nil {
			return stagingv1.UploadStatusResult{}, err
		}
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorStorageUnavailable, true, writeErr)
	}
	before = cloneRecord(*record)
	record.Written, record.ActualDigest = true, written.Digest
	record.Upload.ReceivedSize, record.Upload.UpdatedAt = written.Size, m.now()
	if err := m.saveOrRollbackLocked(ctx, record, before); err != nil {
		return stagingv1.UploadStatusResult{}, err
	}
	return record.result(), nil
}

func (m *Manager) rejectLocked(ctx context.Context, record *uploadRecord, failure string) (stagingv1.UploadStatusResult, error) {
	before := cloneRecord(*record)
	record.Upload.State = stagingv1.StateRejected
	record.Upload.Revision++
	record.Upload.UpdatedAt = m.now()
	record.Content = nil
	record.FailureCode = failure
	if err := m.saveOrRollbackLocked(ctx, record, before); err != nil {
		return stagingv1.UploadStatusResult{}, err
	}
	if err := m.provider.RemoveStaged(ctx, record.Owner, record.Upload.ID); err != nil {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorStorageUnavailable, true, err)
	}
	return record.result(), nil
}
