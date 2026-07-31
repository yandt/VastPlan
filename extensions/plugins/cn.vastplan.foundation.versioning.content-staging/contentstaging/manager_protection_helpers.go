package contentstaging

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	contentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioncontent/v1"
	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
)

func (m *Manager) validateProtectionEntryLocked(ctx context.Context, scope Scope, request contentv1.PrepareRequest, entry contentv1.ContentEntry) error {
	descriptor := stagingv1.ContentDescriptor{Digest: entry.Digest, Size: entry.Size, MediaType: entry.MediaType}
	if entry.UploadID == "" {
		if !m.confirmedContentLocked(scope.TenantID, descriptor) {
			return contentReferenceError(contentv1.ErrorContentUnavailable, false, errors.New("未提供 Upload ID 的内容尚未被已确认版本保护"))
		}
	} else {
		upload, err := m.ownedRecordLocked(scope, entry.UploadID)
		if err != nil || upload.Upload.State != stagingv1.StateReady || upload.Content == nil || !m.now().Before(upload.Upload.LeaseExpiresAt) {
			return contentReferenceError(contentv1.ErrorContentUnavailable, false, errors.New("版本内容来源 Upload 未就绪或已过期"))
		}
		if upload.Upload.SessionID != request.SessionID || upload.Upload.EnvironmentDigest != request.EnvironmentDigest || upload.Upload.Resource != request.Resource || upload.Upload.Path != entry.Path || *upload.Content != descriptor {
			return contentReferenceError(contentv1.ErrorContentUnavailable, false, errors.New("版本内容来源 Upload 与 Workspace 声明不一致"))
		}
	}
	if err := m.provider.VerifyContent(ctx, scope, descriptor); err != nil {
		return contentReferenceError(contentv1.ErrorContentUnavailable, true, err)
	}
	return nil
}

func (m *Manager) confirmedContentLocked(tenantID string, descriptor stagingv1.ContentDescriptor) bool {
	for _, record := range m.protections {
		if record.Owner.TenantID != tenantID || record.Protection.State != contentv1.StateConfirmed {
			continue
		}
		for _, entry := range record.Protection.Entries {
			if entry.Digest == descriptor.Digest && entry.Size == descriptor.Size && entry.MediaType == descriptor.MediaType {
				return true
			}
		}
	}
	return false
}

func (m *Manager) verifyProtectionContentLocked(ctx context.Context, record *protectionRecord) error {
	for _, entry := range record.Protection.Entries {
		descriptor := stagingv1.ContentDescriptor{Digest: entry.Digest, Size: entry.Size, MediaType: entry.MediaType}
		if err := m.provider.VerifyContent(ctx, record.Owner, descriptor); err != nil {
			return contentReferenceError(contentv1.ErrorContentUnavailable, true, err)
		}
	}
	return nil
}

func (m *Manager) ownedProtectionLocked(scope Scope, id string) (*protectionRecord, error) {
	record := m.protections[id]
	if record == nil || record.Owner != scope {
		return nil, contentReferenceError(contentv1.ErrorProtectionNotFound, false, errors.New("版本内容保护不存在"))
	}
	return record, nil
}

func (m *Manager) protectionByOperationLocked(scope Scope, operationID string, stream versioningv1.StreamKey) *protectionRecord {
	for _, record := range m.protections {
		if record.Owner == scope && record.Protection.OperationID == operationID && record.Protection.Stream == stream {
			return record
		}
	}
	return nil
}

func (m *Manager) preparedCountLocked(tenantID string) int {
	count := 0
	for _, record := range m.protections {
		if record.Owner.TenantID == tenantID && record.Protection.State == contentv1.StatePrepared {
			count++
		}
	}
	return count
}

func protectionID(scope Scope, operationID string, stream versioningv1.StreamKey) string {
	sum := sha256.Sum256([]byte(scope.TenantID + "\x00" + scope.ActorID + "\x00" + stream.Namespace + "\x00" + stream.StreamID + "\x00" + operationID))
	return "vcr_" + base64.RawURLEncoding.EncodeToString(sum[:24])
}

func publicEntries(entries []contentv1.ContentEntry) []contentv1.ContentEntry {
	result := make([]contentv1.ContentEntry, len(entries))
	for index, entry := range entries {
		entry.UploadID = ""
		result[index] = entry
	}
	return result
}

func sameLogicalProtection(protection contentv1.Protection, request contentv1.PrepareRequest) bool {
	if protection.OperationID != request.OperationID || protection.EnvironmentDigest != request.EnvironmentDigest || protection.Resource != request.Resource || protection.Stream != request.Stream || protection.ManifestDigest != request.ManifestDigest {
		return false
	}
	entries := publicEntries(request.Entries)
	if len(protection.Entries) != len(entries) {
		return false
	}
	for index := range entries {
		if protection.Entries[index] != entries[index] {
			return false
		}
	}
	return true
}

func cloneProtection(value contentv1.Protection) contentv1.Protection {
	return cloneProtectionRecord(protectionRecord{Protection: value}).Protection
}
