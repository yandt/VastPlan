package contentstaging

import (
	"context"
	"errors"

	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
)

func (m *Manager) Complete(ctx context.Context, scope Scope, request stagingv1.UploadRevisionRequest) (stagingv1.UploadStatusResult, error) {
	if err := scope.Validate(); err != nil {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorInvalidRequest, false, err)
	}
	if err := stagingv1.ValidateUploadRevisionRequest(request); err != nil {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorInvalidRequest, false, err)
	}
	m.mu.Lock()
	record, err := m.ownedRecordLocked(scope, request.UploadID)
	if err != nil {
		m.mu.Unlock()
		return stagingv1.UploadStatusResult{}, err
	}
	if record.Upload.State == stagingv1.StateReady || record.Upload.State == stagingv1.StateRejected {
		result := record.result()
		m.mu.Unlock()
		return result, nil
	}
	if expired, err := m.expireIfNeededLocked(ctx, record); err != nil {
		m.mu.Unlock()
		return stagingv1.UploadStatusResult{}, err
	} else if expired {
		m.mu.Unlock()
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorLeaseExpired, false, errors.New("Content Staging Lease 已过期"))
	}
	if _, busy := m.writers[request.UploadID]; busy {
		m.mu.Unlock()
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorLeaseConflict, true, errors.New("Upload 数据流尚未结束"))
	}
	if record.Upload.State != stagingv1.StateVerifying {
		if record.Upload.Revision != request.ExpectedRevision {
			m.mu.Unlock()
			return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorLeaseConflict, false, errors.New("Upload revision CAS 冲突"))
		}
		if !record.Written {
			m.mu.Unlock()
			return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorDataIncomplete, false, errors.New("Upload 尚无完整数据流"))
		}
		if record.Upload.ReceivedSize != record.Upload.ExpectedSize {
			result, rejectErr := m.rejectLocked(ctx, record, stagingv1.FailureSizeMismatch)
			m.mu.Unlock()
			return result, rejectErr
		}
		if record.ActualDigest != record.Upload.ExpectedDigest {
			result, rejectErr := m.rejectLocked(ctx, record, stagingv1.FailureDigestMismatch)
			m.mu.Unlock()
			return result, rejectErr
		}
		before := cloneRecord(*record)
		record.Upload.State = stagingv1.StateVerifying
		record.Upload.Revision++
		record.Upload.UpdatedAt = m.now()
		if err := m.saveOrRollbackLocked(ctx, record, before); err != nil {
			m.mu.Unlock()
			return stagingv1.UploadStatusResult{}, err
		}
	}
	admissionRevision := record.Upload.Revision
	admissionRequest := AdmissionRequest{Scope: scope, Upload: record.Upload}
	m.mu.Unlock()

	reader, openErr := m.provider.OpenStaged(ctx, scope, request.UploadID)
	if openErr != nil {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorStorageUnavailable, true, openErr)
	}
	admissionErr := m.admission.Admit(ctx, admissionRequest, reader)
	closeErr := reader.Close()
	if ctx.Err() != nil || closeErr != nil {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorStorageUnavailable, true, errors.Join(ctx.Err(), closeErr))
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	record, err = m.ownedRecordLocked(scope, request.UploadID)
	if err != nil {
		return stagingv1.UploadStatusResult{}, err
	}
	if record.Upload.State != stagingv1.StateVerifying || record.Upload.Revision != admissionRevision {
		return record.result(), stagingError(stagingv1.ErrorLeaseConflict, false, errors.New("安全准入期间 Upload 状态已变化"))
	}
	if admissionErr != nil {
		return m.rejectLocked(ctx, record, stagingv1.FailureAdmissionRejected)
	}
	descriptor := stagingv1.ContentDescriptor{Digest: record.Upload.ExpectedDigest, Size: record.Upload.ExpectedSize, MediaType: record.Upload.MediaType}
	if err := m.provider.Promote(ctx, scope, request.UploadID, descriptor); err != nil {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorStorageUnavailable, true, err)
	}
	before := cloneRecord(*record)
	record.Upload.State = stagingv1.StateReady
	record.Upload.Revision++
	record.Upload.UpdatedAt = m.now()
	record.Content = &descriptor
	record.FailureCode = ""
	if err := m.saveOrRollbackLocked(ctx, record, before); err != nil {
		return stagingv1.UploadStatusResult{}, err
	}
	// The object has a durable CAS identity before staging bytes are removed.
	// A cleanup failure is retryable maintenance work, not a failed completion.
	_ = m.provider.RemoveStaged(ctx, scope, request.UploadID)
	return record.result(), nil
}

func (m *Manager) Abort(ctx context.Context, scope Scope, request stagingv1.UploadRevisionRequest) (stagingv1.UploadStatusResult, error) {
	if err := scope.Validate(); err != nil {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorInvalidRequest, false, err)
	}
	if err := stagingv1.ValidateUploadRevisionRequest(request); err != nil {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorInvalidRequest, false, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.ownedRecordLocked(scope, request.UploadID)
	if err != nil {
		return stagingv1.UploadStatusResult{}, err
	}
	if record.Upload.State == stagingv1.StateAborted {
		return record.result(), nil
	}
	if expired, err := m.expireIfNeededLocked(ctx, record); err != nil {
		return stagingv1.UploadStatusResult{}, err
	} else if expired {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorLeaseExpired, false, errors.New("Content Staging Lease 已过期"))
	}
	if terminalState(record.Upload.State) || record.Upload.Revision != request.ExpectedRevision {
		return stagingv1.UploadStatusResult{}, stagingError(stagingv1.ErrorLeaseConflict, false, errors.New("Upload 不能终止或 revision CAS 冲突"))
	}
	wasReady := record.Upload.State == stagingv1.StateReady
	before := cloneRecord(*record)
	record.Upload.State = stagingv1.StateAborted
	record.Upload.Revision++
	record.Upload.UpdatedAt = m.now()
	record.Content = nil
	record.FailureCode = ""
	if err := m.saveOrRollbackLocked(ctx, record, before); err != nil {
		return stagingv1.UploadStatusResult{}, err
	}
	if err := m.provider.RemoveStaged(ctx, scope, request.UploadID); err != nil {
		return record.result(), stagingError(stagingv1.ErrorStorageUnavailable, true, err)
	}
	if wasReady {
		if err := m.removeUnreferencedContentLocked(ctx, scope, before.Upload.ExpectedDigest); err != nil {
			return record.result(), stagingError(stagingv1.ErrorStorageUnavailable, true, err)
		}
	}
	return record.result(), nil
}
