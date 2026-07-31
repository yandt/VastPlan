package versionworkspace

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

type memoryStaging struct {
	now      func() time.Time
	next     int
	statuses map[string]stagingv1.UploadStatusResult
	last     stagingv1.BeginUploadRequest
}

func newMemoryStaging(now func() time.Time) *memoryStaging {
	return &memoryStaging{now: now, statuses: map[string]stagingv1.UploadStatusResult{}}
}

func (s *memoryStaging) BeginUpload(_ context.Context, request stagingv1.BeginUploadRequest) (stagingv1.UploadStatusResult, error) {
	s.next++
	id := fmt.Sprintf("stg_%016d", s.next)
	now := s.now()
	result := stagingv1.UploadStatusResult{Upload: stagingv1.UploadLease{
		Protocol: stagingv1.Protocol, ID: id, SessionID: request.SessionID, EnvironmentDigest: request.EnvironmentDigest, Resource: request.Resource,
		Path: request.Path, MediaType: request.MediaType, ExpectedDigest: request.ExpectedDigest, ExpectedSize: request.ExpectedSize,
		State: stagingv1.StatePending, Revision: 1, CreatedAt: now, UpdatedAt: now, LeaseExpiresAt: now.Add(time.Duration(request.LeaseSeconds) * time.Second),
	}}
	s.last, s.statuses[id] = request, result
	return result, nil
}

func (s *memoryStaging) UploadStatus(_ context.Context, id string) (stagingv1.UploadStatusResult, error) {
	return s.status(id)
}

func (s *memoryStaging) RenewUpload(_ context.Context, request stagingv1.RenewUploadRequest) (stagingv1.UploadStatusResult, error) {
	result, err := s.status(request.UploadID)
	if err != nil {
		return result, err
	}
	result.Upload.Revision++
	result.Upload.UpdatedAt = s.now()
	result.Upload.LeaseExpiresAt = s.now().Add(time.Duration(request.LeaseSeconds) * time.Second)
	s.statuses[request.UploadID] = result
	return result, nil
}

func (s *memoryStaging) CompleteUpload(_ context.Context, request stagingv1.UploadRevisionRequest) (stagingv1.UploadStatusResult, error) {
	result, err := s.status(request.UploadID)
	if err != nil {
		return result, err
	}
	result.Upload.State = stagingv1.StateReady
	result.Upload.ReceivedSize = result.Upload.ExpectedSize
	result.Upload.Revision += 2
	result.Upload.UpdatedAt = s.now()
	result.Content = &stagingv1.ContentDescriptor{Digest: result.Upload.ExpectedDigest, Size: result.Upload.ExpectedSize, MediaType: result.Upload.MediaType}
	s.statuses[request.UploadID] = result
	return result, nil
}

func (s *memoryStaging) AbortUpload(_ context.Context, request stagingv1.UploadRevisionRequest) (stagingv1.UploadStatusResult, error) {
	result, err := s.status(request.UploadID)
	if err != nil {
		return result, err
	}
	result.Upload.State, result.Upload.Revision, result.Upload.UpdatedAt = stagingv1.StateAborted, result.Upload.Revision+1, s.now()
	result.Content = nil
	s.statuses[request.UploadID] = result
	return result, nil
}

func (s *memoryStaging) status(id string) (stagingv1.UploadStatusResult, error) {
	result, ok := s.statuses[id]
	if !ok {
		return stagingv1.UploadStatusResult{}, &StagingError{Code: stagingv1.ErrorLeaseNotFound, Err: fmt.Errorf("missing %s", id)}
	}
	return result, nil
}

func contentManager(t *testing.T, now *time.Time) *Manager {
	t.Helper()
	catalog := NewCatalog()
	for _, adapter := range []Adapter{NewJSONAdapter(), NewTextAdapter(), NewBlobAdapter(), NewFilesAdapter()} {
		if err := catalog.RegisterAdapter(adapter); err != nil {
			t.Fatal(err)
		}
	}
	if err := catalog.RegisterEnvironment(resourcev1.EnvironmentProfile{
		Protocol: resourcev1.Protocol, ID: "content-development", Revision: 1,
		Bindings: []resourcev1.ResourceBinding{{ResourceType: "script.bundle", Namespace: "script.bundle", Adapter: FilesAdapterID, AllowedModes: []string{resourcev1.ModeSnapshot}, DefaultMode: resourcev1.ModeSnapshot, ProjectionPolicy: resourcev1.ProjectionNone}},
		Limits:   resourcev1.WorkspaceLimits{MaxSessionsPerTenant: 4, MaxLeaseSeconds: 3600, MaxSnapshotBytes: 1 << 20, MaxOverlayBytes: 64 << 20},
	}); err != nil {
		t.Fatal(err)
	}
	sequence := 0
	manager, err := NewManager(catalog, ManagerOptions{Now: func() time.Time { return *now }, NewSessionID: func() (string, error) {
		sequence++
		return fmt.Sprintf("ws_%016d", sequence), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func TestWorkspaceBindsReadyUploadBeforeAcceptingFilesManifest(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	manager, ledger := contentManager(t, &now), newMemoryLedger()
	staging := newMemoryStaging(func() time.Time { return now })
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
	upload, err := manager.BeginContentUpload(context.Background(), scope, staging, begin)
	if err != nil || staging.last.ExpectedSessionRevision != session.Revision || staging.last.EnvironmentDigest != session.EnvironmentDigest {
		t.Fatalf("begin: %+v staged=%+v err=%v", upload, staging.last, err)
	}
	snapshot := manifest(fileEntry(begin.Path, begin.MediaType, begin.ExpectedDigest, begin.ExpectedSize))
	if _, err := manager.WriteSnapshot(context.Background(), scope, workspacev1.WriteSnapshotRequest{SessionID: session.ID, ExpectedRevision: session.Revision, Snapshot: snapshot}); errorCode(err) != workspacev1.ErrorContentUnavailable {
		t.Fatalf("未完成 Upload 不得进入 Manifest: %v", err)
	}
	ready, err := manager.CompleteContentUpload(context.Background(), scope, staging, workspacev1.ContentUploadRevisionRequest{
		SessionID: session.ID, UploadID: upload.Upload.Upload.ID, ExpectedUploadRevision: upload.Upload.Upload.Revision,
	})
	if err != nil || ready.Upload.Upload.State != stagingv1.StateReady {
		t.Fatalf("complete: %+v %v", ready, err)
	}
	updated, err := manager.WriteSnapshot(context.Background(), scope, workspacev1.WriteSnapshotRequest{SessionID: session.ID, ExpectedRevision: session.Revision, Snapshot: snapshot})
	if err != nil || updated.State != workspacev1.StateDirty {
		t.Fatalf("Ready Manifest 未被接受: %+v %v", updated, err)
	}
	secondBegin := workspacev1.BeginContentUploadRequest{SessionID: session.ID, ExpectedRevision: updated.Revision, Path: "second.ts", MediaType: "text/typescript", ExpectedDigest: strings.Repeat("d", 64), ExpectedSize: 32, LeaseSeconds: 300}
	second, _ := manager.BeginContentUpload(context.Background(), scope, staging, secondBegin)
	_, _ = manager.CompleteContentUpload(context.Background(), scope, staging, workspacev1.ContentUploadRevisionRequest{SessionID: session.ID, UploadID: second.Upload.Upload.ID, ExpectedUploadRevision: second.Upload.Upload.Revision})
	secondSnapshot := manifest(
		fileEntry(begin.Path, begin.MediaType, begin.ExpectedDigest, begin.ExpectedSize),
		fileEntry(secondBegin.Path, secondBegin.MediaType, secondBegin.ExpectedDigest, secondBegin.ExpectedSize),
	)
	updated, err = manager.WriteSnapshot(context.Background(), scope, workspacev1.WriteSnapshotRequest{SessionID: session.ID, ExpectedRevision: updated.Revision, Snapshot: secondSnapshot})
	if err != nil {
		t.Fatalf("后续 revision 应保留仍受 Lease 保护的当前内容: %v", err)
	}
	if _, err := manager.Commit(context.Background(), scope, ledger, workspacev1.CommitRequest{SessionID: session.ID, ExpectedRevision: updated.Revision, OperationID: "script-publication:0001"}); errorCode(err) != workspacev1.ErrorOperationUnsupported {
		t.Fatalf("未提供 durable reference 的内部调用路径必须阻止 Files commit: %v", err)
	}
}

func TestWorkspaceRejectsReadyBindingAfterSessionRevisionOrUploadLeaseChanges(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	manager, ledger := contentManager(t, &now), newMemoryLedger()
	staging := newMemoryStaging(func() time.Time { return now })
	scope := Scope{TenantID: "tenant-a", ActorID: "plugin:studio"}
	session, _ := manager.Open(context.Background(), scope, ledger, workspacev1.OpenRequest{EnvironmentID: "content-development", Resource: resourcev1.ResourceKey{Type: "script.bundle", ID: "daily"}, LeaseSeconds: 3600})
	begin := workspacev1.BeginContentUploadRequest{SessionID: session.ID, ExpectedRevision: session.Revision, Path: "main.ts", MediaType: "text/typescript", ExpectedDigest: strings.Repeat("b", 64), ExpectedSize: 64, LeaseSeconds: 60}
	upload, _ := manager.BeginContentUpload(context.Background(), scope, staging, begin)
	_, _ = manager.CompleteContentUpload(context.Background(), scope, staging, workspacev1.ContentUploadRevisionRequest{SessionID: session.ID, UploadID: upload.Upload.Upload.ID, ExpectedUploadRevision: upload.Upload.Upload.Revision})
	snapshot := manifest(fileEntry(begin.Path, begin.MediaType, begin.ExpectedDigest, begin.ExpectedSize))
	renewed, err := manager.Renew(scope, workspacev1.RenewRequest{SessionID: session.ID, ExpectedRevision: session.Revision, LeaseSeconds: 3600})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.WriteSnapshot(context.Background(), scope, workspacev1.WriteSnapshotRequest{SessionID: session.ID, ExpectedRevision: renewed.Revision, Snapshot: snapshot}); errorCode(err) != workspacev1.ErrorContentUnavailable {
		t.Fatalf("旧 Session revision 的 Ready 绑定不得复用: %v", err)
	}

	second, _ := manager.BeginContentUpload(context.Background(), scope, staging, workspacev1.BeginContentUploadRequest{SessionID: session.ID, ExpectedRevision: renewed.Revision, Path: "second.ts", MediaType: "text/typescript", ExpectedDigest: strings.Repeat("c", 64), ExpectedSize: 32, LeaseSeconds: 60})
	_, _ = manager.CompleteContentUpload(context.Background(), scope, staging, workspacev1.ContentUploadRevisionRequest{SessionID: session.ID, UploadID: second.Upload.Upload.ID, ExpectedUploadRevision: second.Upload.Upload.Revision})
	now = now.Add(61 * time.Second)
	expired := manifest(fileEntry("second.ts", "text/typescript", strings.Repeat("c", 64), 32))
	if _, err := manager.WriteSnapshot(context.Background(), scope, workspacev1.WriteSnapshotRequest{SessionID: session.ID, ExpectedRevision: renewed.Revision, Snapshot: expired}); errorCode(err) != workspacev1.ErrorContentUnavailable {
		t.Fatalf("已过期 Ready Lease 不得进入 Manifest: %v", err)
	}
}
