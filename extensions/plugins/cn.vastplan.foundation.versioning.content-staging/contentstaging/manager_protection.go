package contentstaging

import (
	"context"
	"errors"

	contentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioncontent/v1"
)

func (m *Manager) PrepareProtection(ctx context.Context, scope Scope, request contentv1.PrepareRequest) (contentv1.ProtectionResult, error) {
	if m == nil || scope.Validate() != nil || contentv1.ValidatePrepareRequest(request) != nil {
		return contentv1.ProtectionResult{}, contentReferenceError(contentv1.ErrorInvalidRequest, false, errors.New("准备版本内容保护请求无效"))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.protectionByOperationLocked(scope, request.OperationID, request.Stream); existing != nil {
		if !sameLogicalProtection(existing.Protection, request) || existing.Protection.State == contentv1.StateAborted {
			return contentv1.ProtectionResult{}, contentReferenceError(contentv1.ErrorConflict, false, errors.New("operationId 已绑定其他内容保护或已显式终止"))
		}
		if existing.Protection.State == contentv1.StatePrepared {
			if existing.Protection.ExpiresAt == nil || !m.now().Before(*existing.Protection.ExpiresAt) {
				if err := m.reprepareExpiredProtectionLocked(ctx, scope, existing, request); err != nil {
					return contentv1.ProtectionResult{}, err
				}
				return contentv1.ProtectionResult{Protection: cloneProtection(existing.Protection)}, nil
			}
			before := cloneProtectionRecord(*existing)
			expires := m.now().UTC().Add(m.limits.PreparedProtection)
			existing.Protection.Revision++
			existing.Protection.UpdatedAt = m.now().UTC()
			existing.Protection.ExpiresAt = &expires
			if err := m.provider.SaveProtection(ctx, *existing); err != nil {
				*existing = before
				return contentv1.ProtectionResult{}, contentReferenceError(contentv1.ErrorStorageUnavailable, true, err)
			}
		}
		if existing.Protection.State == contentv1.StateExpired {
			if err := m.reprepareExpiredProtectionLocked(ctx, scope, existing, request); err != nil {
				return contentv1.ProtectionResult{}, err
			}
			return contentv1.ProtectionResult{Protection: cloneProtection(existing.Protection)}, nil
		}
		if err := m.verifyProtectionContentLocked(ctx, existing); err != nil {
			return contentv1.ProtectionResult{}, err
		}
		return contentv1.ProtectionResult{Protection: cloneProtection(existing.Protection)}, nil
	}
	if m.preparedCountLocked(scope.TenantID) >= m.limits.MaxPreparedPerTenant {
		return contentv1.ProtectionResult{}, contentReferenceError(contentv1.ErrorLimitExceeded, false, errors.New("租户待确认版本内容保护已达到上限"))
	}
	sourceUploads := make(map[string]string)
	for _, entry := range request.Entries {
		if err := m.validateProtectionEntryLocked(ctx, scope, request, entry); err != nil {
			return contentv1.ProtectionResult{}, err
		}
		if entry.UploadID != "" {
			sourceUploads[entry.Path] = entry.UploadID
		}
	}
	now := m.now().UTC()
	expires := now.Add(m.limits.PreparedProtection)
	entries := publicEntries(request.Entries)
	record := protectionRecord{
		FormatVersion: protectionRecordFormatVersion, Owner: scope, SourceSessionID: request.SessionID,
		SourceSessionRevision: request.ExpectedSessionRevision, SourceUploadIDs: sourceUploads,
		Protection: contentv1.Protection{
			Protocol: contentv1.Protocol, ID: protectionID(scope, request.OperationID, request.Stream), OperationID: request.OperationID,
			EnvironmentDigest: request.EnvironmentDigest, Resource: request.Resource, Stream: request.Stream,
			ManifestDigest: request.ManifestDigest, Entries: entries, State: contentv1.StatePrepared,
			Revision: 1, CreatedAt: now, UpdatedAt: now, ExpiresAt: &expires,
		},
	}
	if err := m.provider.SaveProtection(ctx, record); err != nil {
		return contentv1.ProtectionResult{}, contentReferenceError(contentv1.ErrorStorageUnavailable, true, err)
	}
	m.protections[record.Protection.ID] = &record
	return contentv1.ProtectionResult{Protection: cloneProtection(record.Protection)}, nil
}

func (m *Manager) reprepareExpiredProtectionLocked(ctx context.Context, scope Scope, record *protectionRecord, request contentv1.PrepareRequest) error {
	if record.Protection.State == contentv1.StateExpired && m.preparedCountLocked(scope.TenantID) >= m.limits.MaxPreparedPerTenant {
		return contentReferenceError(contentv1.ErrorLimitExceeded, false, errors.New("租户待确认版本内容保护已达到上限"))
	}
	sourceUploads := make(map[string]string)
	for _, entry := range request.Entries {
		if err := m.validateProtectionEntryLocked(ctx, scope, request, entry); err != nil {
			return err
		}
		if entry.UploadID != "" {
			sourceUploads[entry.Path] = entry.UploadID
		}
	}
	before := cloneProtectionRecord(*record)
	now := m.now().UTC()
	expires := now.Add(m.limits.PreparedProtection)
	record.SourceSessionID = request.SessionID
	record.SourceSessionRevision = request.ExpectedSessionRevision
	record.SourceUploadIDs = sourceUploads
	record.Protection.State = contentv1.StatePrepared
	record.Protection.Revision++
	record.Protection.Version = nil
	record.Protection.UpdatedAt = now
	record.Protection.ExpiresAt = &expires
	if err := m.provider.SaveProtection(ctx, *record); err != nil {
		*record = before
		return contentReferenceError(contentv1.ErrorStorageUnavailable, true, err)
	}
	return nil
}

func (m *Manager) ProtectionStatus(ctx context.Context, scope Scope, request contentv1.StatusRequest) (contentv1.ProtectionResult, error) {
	if m == nil || scope.Validate() != nil || contentv1.ValidateStatusRequest(request) != nil {
		return contentv1.ProtectionResult{}, contentReferenceError(contentv1.ErrorInvalidRequest, false, errors.New("读取版本内容保护请求无效"))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.ownedProtectionLocked(scope, request.ProtectionID)
	if err != nil {
		return contentv1.ProtectionResult{}, err
	}
	if protectionActive(record.Protection.State) {
		if err := m.verifyProtectionContentLocked(ctx, record); err != nil {
			return contentv1.ProtectionResult{}, err
		}
	}
	return contentv1.ProtectionResult{Protection: cloneProtection(record.Protection)}, nil
}

func (m *Manager) ConfirmProtection(ctx context.Context, scope Scope, request contentv1.ConfirmRequest) (contentv1.ProtectionResult, error) {
	if m == nil || scope.Validate() != nil || contentv1.ValidateConfirmRequest(request) != nil {
		return contentv1.ProtectionResult{}, contentReferenceError(contentv1.ErrorInvalidRequest, false, errors.New("确认版本内容保护请求无效"))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.ownedProtectionLocked(scope, request.ProtectionID)
	if err != nil {
		return contentv1.ProtectionResult{}, err
	}
	if record.Protection.State == contentv1.StateConfirmed {
		if record.Protection.Version != nil && *record.Protection.Version == request.Version {
			if err := m.verifyProtectionContentLocked(ctx, record); err != nil {
				return contentv1.ProtectionResult{}, err
			}
			return contentv1.ProtectionResult{Protection: cloneProtection(record.Protection)}, nil
		}
		return contentv1.ProtectionResult{}, contentReferenceError(contentv1.ErrorConflict, false, errors.New("内容保护已确认给其他版本"))
	}
	if record.Protection.State != contentv1.StatePrepared || record.Protection.Revision != request.ExpectedRevision {
		return contentv1.ProtectionResult{}, contentReferenceError(contentv1.ErrorConflict, false, errors.New("内容保护状态或 revision 冲突"))
	}
	if request.Version.Stream != record.Protection.Stream || request.Version.ContentDigest != record.Protection.ManifestDigest {
		return contentv1.ProtectionResult{}, contentReferenceError(contentv1.ErrorConflict, false, errors.New("VersionRef 与准备的 stream 或 manifest 摘要不一致"))
	}
	if err := m.verifyProtectionContentLocked(ctx, record); err != nil {
		return contentv1.ProtectionResult{}, err
	}
	before := cloneProtectionRecord(*record)
	now := m.now().UTC()
	version := request.Version
	record.Protection.State = contentv1.StateConfirmed
	record.Protection.Revision++
	record.Protection.Version = &version
	record.Protection.UpdatedAt = now
	record.Protection.ExpiresAt = nil
	if err := m.provider.SaveProtection(ctx, *record); err != nil {
		*record = before
		return contentv1.ProtectionResult{}, contentReferenceError(contentv1.ErrorStorageUnavailable, true, err)
	}
	return contentv1.ProtectionResult{Protection: cloneProtection(record.Protection)}, nil
}

func (m *Manager) AbortProtection(ctx context.Context, scope Scope, request contentv1.AbortRequest) (contentv1.ProtectionResult, error) {
	if m == nil || scope.Validate() != nil || contentv1.ValidateAbortRequest(request) != nil {
		return contentv1.ProtectionResult{}, contentReferenceError(contentv1.ErrorInvalidRequest, false, errors.New("终止版本内容保护请求无效"))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.ownedProtectionLocked(scope, request.ProtectionID)
	if err != nil {
		return contentv1.ProtectionResult{}, err
	}
	if record.Protection.State == contentv1.StateAborted {
		return contentv1.ProtectionResult{Protection: cloneProtection(record.Protection)}, nil
	}
	if record.Protection.State != contentv1.StatePrepared || record.Protection.Revision != request.ExpectedRevision {
		return contentv1.ProtectionResult{}, contentReferenceError(contentv1.ErrorConflict, false, errors.New("只有匹配 revision 的 Prepared 保护可以终止"))
	}
	before := cloneProtectionRecord(*record)
	record.Protection.State = contentv1.StateAborted
	record.Protection.Revision++
	record.Protection.UpdatedAt = m.now().UTC()
	record.Protection.ExpiresAt = nil
	if err := m.provider.SaveProtection(ctx, *record); err != nil {
		*record = before
		return contentv1.ProtectionResult{}, contentReferenceError(contentv1.ErrorStorageUnavailable, true, err)
	}
	for _, entry := range record.Protection.Entries {
		if err := m.removeUnreferencedContentLocked(ctx, record.Owner, entry.Digest); err != nil {
			return contentv1.ProtectionResult{}, contentReferenceError(contentv1.ErrorStorageUnavailable, true, err)
		}
	}
	return contentv1.ProtectionResult{Protection: cloneProtection(record.Protection)}, nil
}
