package contentstaging

import (
	"context"
	"errors"
	"fmt"
	"strings"

	contentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioncontent/v1"
	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
)

func validateStoredProtection(record protectionRecord) error {
	if record.FormatVersion != protectionRecordFormatVersion || record.Owner.Validate() != nil || contentv1.ValidateProtection(record.Protection) != nil {
		return errors.New("持久化 Content Reference 记录无效")
	}
	entries := append([]contentv1.ContentEntry(nil), record.Protection.Entries...)
	entryByPath := make(map[string]*contentv1.ContentEntry, len(entries))
	for index := range entries {
		entryByPath[entries[index].Path] = &entries[index]
	}
	for path, id := range record.SourceUploadIDs {
		entry := entryByPath[path]
		if strings.TrimSpace(path) == "" || entry == nil || validateUploadID(id) != nil {
			return errors.New("持久化 Content Reference 来源无效")
		}
		entry.UploadID = id
	}
	if contentv1.ValidatePrepareRequest(contentv1.PrepareRequest{
		OperationID: record.Protection.OperationID, SessionID: record.SourceSessionID, ExpectedSessionRevision: record.SourceSessionRevision,
		EnvironmentDigest: record.Protection.EnvironmentDigest, Resource: record.Protection.Resource, Stream: record.Protection.Stream,
		ManifestDigest: record.Protection.ManifestDigest, Entries: entries,
	}) != nil {
		return errors.New("持久化 Content Reference 来源请求无效")
	}
	return nil
}

func validateStoredRecord(record uploadRecord) error {
	if record.FormatVersion != uploadRecordFormatVersion || record.Owner.Validate() != nil || !validSHA256(record.BeginDigest) || record.SessionRevision == 0 ||
		record.BeginLeaseSeconds < stagingv1.MinimumLeaseSeconds || record.BeginLeaseSeconds > stagingv1.MaximumLeaseSeconds || stagingv1.ValidateUploadStatusResult(record.result()) != nil {
		return errors.New("持久化 Upload 记录无效")
	}
	if record.Upload.State == stagingv1.StateReady && (!record.Written || record.ActualDigest != record.Upload.ExpectedDigest) {
		return errors.New("Ready Upload 缺少已校验内容")
	}
	if record.Written && (!validSHA256(record.ActualDigest) || record.Upload.ReceivedSize < 0) {
		return errors.New("Upload 写入结果无效")
	}
	return nil
}

func (m *Manager) verifyReadyContent(ctx context.Context) error {
	for _, record := range m.uploads {
		if record.Upload.State == stagingv1.StateReady {
			if err := m.provider.VerifyContent(ctx, record.Owner, *record.Content); err != nil {
				return fmt.Errorf("Ready Content %s 不可用: %w", record.Upload.ID, err)
			}
			if err := m.provider.RemoveStaged(ctx, record.Owner, record.Upload.ID); err != nil {
				return fmt.Errorf("清理 Ready Content %s 的暂存副本: %w", record.Upload.ID, err)
			}
		}
	}
	for _, record := range m.protections {
		if !protectionActive(record.Protection.State) {
			continue
		}
		for _, entry := range record.Protection.Entries {
			descriptor := stagingv1.ContentDescriptor{Digest: entry.Digest, Size: entry.Size, MediaType: entry.MediaType}
			if err := m.provider.VerifyContent(ctx, record.Owner, descriptor); err != nil {
				return fmt.Errorf("Content Reference %s 的对象 %s 不可用: %w", record.Protection.ID, entry.Digest, err)
			}
		}
	}
	return nil
}
