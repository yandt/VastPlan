package versionworkspace

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	contentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioncontent/v1"
	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

type memoryContentReference struct {
	protection      contentv1.Protection
	prepareRequest  contentv1.PrepareRequest
	prepareCalls    int
	confirmCalls    int
	failConfirmOnce bool
}

func (r *memoryContentReference) Prepare(_ context.Context, request contentv1.PrepareRequest) (contentv1.ProtectionResult, error) {
	r.prepareCalls++
	if r.protection.ID != "" {
		return contentv1.ProtectionResult{Protection: r.protection}, nil
	}
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour)
	entries := publicEntriesForTest(request.Entries)
	r.prepareRequest = request
	r.protection = contentv1.Protection{
		Protocol: contentv1.Protocol, ID: "vcr_1234567890abcdef", OperationID: request.OperationID,
		EnvironmentDigest: request.EnvironmentDigest, Resource: request.Resource, Stream: request.Stream,
		ManifestDigest: request.ManifestDigest, Entries: entries, State: contentv1.StatePrepared, Revision: 1,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: &expires,
	}
	return contentv1.ProtectionResult{Protection: r.protection}, nil
}

func (r *memoryContentReference) Status(_ context.Context, _ string) (contentv1.ProtectionResult, error) {
	return contentv1.ProtectionResult{Protection: r.protection}, nil
}

func (r *memoryContentReference) Confirm(_ context.Context, request contentv1.ConfirmRequest) (contentv1.ProtectionResult, error) {
	r.confirmCalls++
	if r.protection.State != contentv1.StateConfirmed {
		now := r.protection.UpdatedAt.Add(time.Second)
		version := request.Version
		r.protection.State, r.protection.Revision, r.protection.Version = contentv1.StateConfirmed, r.protection.Revision+1, &version
		r.protection.UpdatedAt, r.protection.ExpiresAt = now, nil
		if r.failConfirmOnce {
			r.failConfirmOnce = false
			return contentv1.ProtectionResult{}, &ContentReferenceError{Code: contentv1.ErrorStorageUnavailable, Retryable: true, Err: errors.New("confirmation response lost")}
		}
	}
	if r.protection.Version == nil || *r.protection.Version != request.Version {
		return contentv1.ProtectionResult{}, &ContentReferenceError{Code: contentv1.ErrorConflict, Err: errors.New("version mismatch")}
	}
	return contentv1.ProtectionResult{Protection: r.protection}, nil
}

func (r *memoryContentReference) Abort(_ context.Context, _ contentv1.AbortRequest) (contentv1.ProtectionResult, error) {
	return contentv1.ProtectionResult{}, errors.New("not used")
}

func TestFilesCommitRetriesAcrossLostConfirmationWithoutDuplicatingVersion(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	manager, ledger := contentManager(t, &now), newMemoryLedger()
	staging := newMemoryStaging(func() time.Time { return now })
	references := &memoryContentReference{failConfirmOnce: true}
	scope := Scope{TenantID: "tenant-a", ActorID: "plugin:studio"}
	session, err := manager.Open(context.Background(), scope, ledger, workspacev1.OpenRequest{
		EnvironmentID: "content-development", Resource: resourcev1.ResourceKey{Type: "script.bundle", ID: "daily"}, LeaseSeconds: 3600,
	})
	if err != nil {
		t.Fatal(err)
	}
	begin := workspacev1.BeginContentUploadRequest{
		SessionID: session.ID, ExpectedRevision: session.Revision, Path: "main.ts", MediaType: "text/typescript",
		ExpectedDigest: strings.Repeat("a", 64), ExpectedSize: 128, LeaseSeconds: 300,
	}
	upload, _ := manager.BeginContentUpload(context.Background(), scope, staging, begin)
	_, _ = manager.CompleteContentUpload(context.Background(), scope, staging, workspacev1.ContentUploadRevisionRequest{SessionID: session.ID, UploadID: upload.Upload.Upload.ID, ExpectedUploadRevision: upload.Upload.Upload.Revision})
	updated, err := manager.WriteSnapshot(context.Background(), scope, workspacev1.WriteSnapshotRequest{SessionID: session.ID, ExpectedRevision: session.Revision, Snapshot: manifest(fileEntry(begin.Path, begin.MediaType, begin.ExpectedDigest, begin.ExpectedSize))})
	if err != nil {
		t.Fatal(err)
	}
	commit := workspacev1.CommitRequest{SessionID: session.ID, ExpectedRevision: updated.Revision, OperationID: "script-publication:0001"}
	if _, err := manager.CommitWithContent(context.Background(), scope, ledger, references, commit); errorCode(err) != workspacev1.ErrorContentUnavailable {
		t.Fatalf("首次确认响应丢失应返回可重试内容错误: %v", err)
	}
	if len(ledger.versions) != 1 || references.protection.State != contentv1.StateConfirmed {
		t.Fatalf("响应丢失前应已持久提交版本与引用: versions=%d protection=%+v", len(ledger.versions), references.protection)
	}
	result, err := manager.CommitWithContent(context.Background(), scope, ledger, references, commit)
	if err != nil || result.Version.Ref != *references.protection.Version || len(ledger.versions) != 1 || ledger.nextSequence != 1 {
		t.Fatalf("幂等重试产生重复或未完成: result=%+v versions=%d sequence=%d err=%v", result, len(ledger.versions), ledger.nextSequence, err)
	}
	if references.prepareCalls != 2 || references.confirmCalls != 2 || len(references.prepareRequest.Entries) != 1 || references.prepareRequest.Entries[0].UploadID == "" {
		t.Fatalf("Workspace 未以同一事务链重试: %+v", references)
	}
}

func publicEntriesForTest(entries []contentv1.ContentEntry) []contentv1.ContentEntry {
	result := append([]contentv1.ContentEntry(nil), entries...)
	for index := range result {
		result[index].UploadID = ""
	}
	return result
}
