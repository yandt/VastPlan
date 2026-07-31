package contentstaging

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioncontent/v1"
	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
)

func TestPreparedProtectionSurvivesUploadExpiryAndConfirmsIdempotently(t *testing.T) {
	fixture := newManagerFixture(t, IntegrityAdmission{})
	content := []byte("durable-version-content")
	upload := readyUpload(t, fixture, content)
	request := protectionRequest(upload, strings.Repeat("f", 64))
	prepared, err := fixture.manager.PrepareProtection(context.Background(), testScope(), request)
	if err != nil || prepared.Protection.State != contentv1.StatePrepared || prepared.Protection.Entries[0].UploadID != "" {
		t.Fatalf("prepare: %+v %v", prepared, err)
	}
	object := filepath.Join(fixture.root, "tenants", pathDigest(testScope().TenantID), "objects", "sha256", upload.Content.Digest)
	fixture.now = fixture.now.Add(61 * time.Second)
	if _, err := fixture.manager.Reclaim(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(object); err != nil {
		t.Fatalf("Prepared 保护期间 CAS 对象被回收: %v", err)
	}
	version := versioningv1.VersionRef{Stream: request.Stream, VersionID: strings.Repeat("a", 64), Sequence: 1, ContentDigest: request.ManifestDigest}
	confirmed, err := fixture.manager.ConfirmProtection(context.Background(), testScope(), contentv1.ConfirmRequest{ProtectionID: prepared.Protection.ID, ExpectedRevision: prepared.Protection.Revision, Version: version})
	if err != nil || confirmed.Protection.State != contentv1.StateConfirmed || confirmed.Protection.ExpiresAt != nil {
		t.Fatalf("confirm: %+v %v", confirmed, err)
	}
	// A lost confirmation response is retried with the original CAS revision.
	retried, err := fixture.manager.ConfirmProtection(context.Background(), testScope(), contentv1.ConfirmRequest{ProtectionID: prepared.Protection.ID, ExpectedRevision: prepared.Protection.Revision, Version: version})
	if err != nil || retried.Protection.Revision != confirmed.Protection.Revision {
		t.Fatalf("idempotent confirm: %+v %v", retried, err)
	}
	// A reopened Workspace may use a different Session and no temporary ID;
	// logical operation identity still resolves the same durable protection.
	reopen := request
	reopen.SessionID = "ws_qrstuvwxyzabcdef"
	reopen.ExpectedSessionRevision = 7
	reopen.Entries[0].UploadID = ""
	reused, err := fixture.manager.PrepareProtection(context.Background(), testScope(), reopen)
	if err != nil || reused.Protection.ID != confirmed.Protection.ID || reused.Protection.State != contentv1.StateConfirmed {
		t.Fatalf("cross-session idempotent prepare: %+v %v", reused, err)
	}
	fixture.now = fixture.now.Add(48 * time.Hour)
	if _, err := fixture.manager.Reclaim(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewManager(context.Background(), fixture.provider, IntegrityAdmission{}, ManagerOptions{Limits: fixture.limits, Now: func() time.Time { return fixture.now }})
	if err != nil {
		t.Fatal(err)
	}
	status, err := restarted.ProtectionStatus(context.Background(), testScope(), contentv1.StatusRequest{ProtectionID: confirmed.Protection.ID})
	if err != nil || status.Protection.State != contentv1.StateConfirmed {
		t.Fatalf("restart status: %+v %v", status, err)
	}
	if _, err := os.Stat(object); err != nil {
		t.Fatalf("Confirmed 版本引用未永久保护 CAS 对象: %v", err)
	}
}

func TestPreparedProtectionExpiresAndEventuallyReleasesContent(t *testing.T) {
	fixture := newManagerFixture(t, IntegrityAdmission{})
	content := []byte("abandoned-content")
	upload := readyUpload(t, fixture, content)
	prepared, err := fixture.manager.PrepareProtection(context.Background(), testScope(), protectionRequest(upload, strings.Repeat("e", 64)))
	if err != nil {
		t.Fatal(err)
	}
	fixture.now = fixture.now.Add(fixture.limits.PreparedProtection + time.Second)
	if _, err := fixture.manager.Reclaim(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, err := fixture.manager.ProtectionStatus(context.Background(), testScope(), contentv1.StatusRequest{ProtectionID: prepared.Protection.ID})
	if err != nil || status.Protection.State != contentv1.StateExpired {
		t.Fatalf("prepared expiry: %+v %v", status, err)
	}
	object := filepath.Join(fixture.root, "tenants", pathDigest(testScope().TenantID), "objects", "sha256", upload.Content.Digest)
	if _, err := os.Stat(object); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Upload 与 Prepared 均过期后对象未释放: %v", err)
	}
	reuploaded := readyUploadWithKey(t, fixture, content, "upload-retry")
	retryRequest := protectionRequest(reuploaded, strings.Repeat("e", 64))
	recovered, err := fixture.manager.PrepareProtection(context.Background(), testScope(), retryRequest)
	if err != nil || recovered.Protection.ID != prepared.Protection.ID || recovered.Protection.State != contentv1.StatePrepared || recovered.Protection.Revision <= status.Protection.Revision {
		t.Fatalf("同一 operationId 重新上传后未恢复保护: %+v %v", recovered, err)
	}
}

func readyUpload(t *testing.T, fixture *managerFixture, content []byte) stagingv1.UploadStatusResult {
	return readyUploadWithKey(t, fixture, content, "upload-ready")
}

func readyUploadWithKey(t *testing.T, fixture *managerFixture, content []byte, idempotencyKey string) stagingv1.UploadStatusResult {
	t.Helper()
	created, err := fixture.manager.Begin(context.Background(), testScope(), idempotencyKey, beginRequest(content))
	if err != nil {
		t.Fatal(err)
	}
	written, err := fixture.manager.Stream(context.Background(), testScope(), created.Upload.ID, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	ready, err := fixture.manager.Complete(context.Background(), testScope(), stagingv1.UploadRevisionRequest{UploadID: created.Upload.ID, ExpectedRevision: written.Upload.Revision})
	if err != nil {
		t.Fatal(err)
	}
	return ready
}

func protectionRequest(upload stagingv1.UploadStatusResult, manifestDigest string) contentv1.PrepareRequest {
	return contentv1.PrepareRequest{
		OperationID: "workspace-commit:0001", SessionID: upload.Upload.SessionID, ExpectedSessionRevision: 2,
		EnvironmentDigest: upload.Upload.EnvironmentDigest, Resource: upload.Upload.Resource,
		Stream: versioningv1.StreamKey{Namespace: "script.bundle", StreamID: upload.Upload.Resource.ID}, ManifestDigest: manifestDigest,
		Entries: []contentv1.ContentEntry{{Path: upload.Upload.Path, UploadID: upload.Upload.ID, Digest: upload.Content.Digest, Size: upload.Content.Size, MediaType: upload.Content.MediaType}},
	}
}
