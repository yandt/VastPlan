package contentstaging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
)

type rejectingAdmission struct{}

func (rejectingAdmission) Admit(context.Context, AdmissionRequest, io.Reader) error {
	return errors.New("policy rejected")
}

type managerFixture struct {
	manager  *Manager
	provider *FileProvider
	root     string
	now      time.Time
	limits   Limits
}

func newManagerFixture(t *testing.T, admission Admission) *managerFixture {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := &managerFixture{
		root: root, now: time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC),
		limits: Limits{MaxFileBytes: 1024, MaxTenantBytes: 2048, MaxTotalBytes: 4096, MaxActiveUploadsPerTenant: 2, MaxLeaseSeconds: 300, MaxPreparedPerTenant: 8, PreparedProtection: time.Hour, TerminalRetention: time.Hour},
	}
	provider, err := OpenFileProvider(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	fixture.provider = provider
	manager, err := NewManager(context.Background(), provider, admission, ManagerOptions{Limits: fixture.limits, Now: func() time.Time { return fixture.now }})
	if err != nil {
		t.Fatal(err)
	}
	fixture.manager = manager
	return fixture
}

func testScope() Scope { return Scope{TenantID: "tenant-a", ActorID: "user:alice"} }

func beginRequest(content []byte) stagingv1.BeginUploadRequest {
	return stagingv1.BeginUploadRequest{
		SessionID: "ws_abcdefghijklmnop", ExpectedSessionRevision: 1, EnvironmentDigest: strings.Repeat("e", 64),
		Resource: resourcev1.ResourceKey{Type: "portal.configuration", ID: "portal-main"}, Path: "assets/main.bin", MediaType: "application/octet-stream",
		ExpectedDigest: contentDigest(content), ExpectedSize: int64(len(content)), LeaseSeconds: 60,
	}
}

func contentDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func TestManagerStreamsCompletesAndRecoversReadyContent(t *testing.T) {
	fixture := newManagerFixture(t, IntegrityAdmission{})
	content := []byte("streamed-content")
	created, err := fixture.manager.Begin(context.Background(), testScope(), "upload-1", beginRequest(content))
	if err != nil || created.Upload.State != stagingv1.StatePending {
		t.Fatalf("begin: %+v %v", created, err)
	}
	retriedBegin, err := fixture.manager.Begin(context.Background(), testScope(), "upload-1", beginRequest(content))
	if err != nil || retriedBegin.Upload.ID != created.Upload.ID {
		t.Fatalf("idempotent begin: %+v %v", retriedBegin, err)
	}
	changed := beginRequest(content)
	changed.Path = "assets/other.bin"
	if _, err := fixture.manager.Begin(context.Background(), testScope(), "upload-1", changed); errorCode(err) != stagingv1.ErrorLeaseConflict {
		t.Fatalf("相同 idempotency key 不得绑定不同声明: %v", err)
	}
	changed = beginRequest(content)
	changed.ExpectedSessionRevision++
	if _, err := fixture.manager.Begin(context.Background(), testScope(), "upload-1", changed); errorCode(err) != stagingv1.ErrorLeaseConflict {
		t.Fatalf("相同 idempotency key 不得跨 Session revision: %v", err)
	}
	written, err := fixture.manager.Stream(context.Background(), testScope(), created.Upload.ID, bytes.NewReader(content))
	if err != nil || written.Upload.State != stagingv1.StateUploading || written.Upload.ReceivedSize != int64(len(content)) || written.Upload.Revision != created.Upload.Revision {
		t.Fatalf("stream: %+v %v", written, err)
	}
	ready, err := fixture.manager.Complete(context.Background(), testScope(), stagingv1.UploadRevisionRequest{UploadID: created.Upload.ID, ExpectedRevision: written.Upload.Revision})
	if err != nil || ready.Upload.State != stagingv1.StateReady || ready.Content == nil || ready.Content.Digest != contentDigest(content) {
		t.Fatalf("complete: %+v %v", ready, err)
	}
	reader, err := fixture.provider.OpenContent(context.Background(), testScope(), *ready.Content)
	if err != nil {
		t.Fatal(err)
	}
	opened, readErr := io.ReadAll(reader)
	if err := errors.Join(readErr, reader.Close()); err != nil || !bytes.Equal(opened, content) {
		t.Fatalf("open content: %q %v", opened, err)
	}
	if _, err := os.Stat(filepath.Join(fixture.root, "tenants", pathDigest(testScope().TenantID), "staging", created.Upload.ID+".part")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ready 后暂存文件未清理: %v", err)
	}
	restarted, err := NewManager(context.Background(), fixture.provider, IntegrityAdmission{}, ManagerOptions{Limits: fixture.limits, Now: func() time.Time { return fixture.now }})
	if err != nil {
		t.Fatal(err)
	}
	status, err := restarted.Status(context.Background(), testScope(), stagingv1.UploadStatusRequest{UploadID: created.Upload.ID})
	if err != nil || status.Upload.State != stagingv1.StateReady || status.Content == nil {
		t.Fatalf("restart status: %+v %v", status, err)
	}
	// Lost complete responses are idempotent even when the caller only has the
	// pre-verification revision.
	retried, err := restarted.Complete(context.Background(), testScope(), stagingv1.UploadRevisionRequest{UploadID: created.Upload.ID, ExpectedRevision: created.Upload.Revision})
	if err != nil || retried.Upload.Revision != ready.Upload.Revision {
		t.Fatalf("idempotent complete: %+v %v", retried, err)
	}
}

func TestManagerRejectsSizeDigestAndAdmissionFailures(t *testing.T) {
	t.Run("overflow", func(t *testing.T) {
		fixture := newManagerFixture(t, IntegrityAdmission{})
		request := beginRequest([]byte("abc"))
		created, _ := fixture.manager.Begin(context.Background(), testScope(), "upload-1", request)
		result, err := fixture.manager.Stream(context.Background(), testScope(), created.Upload.ID, strings.NewReader("abcd"))
		if err != nil || result.Upload.State != stagingv1.StateRejected || result.FailureCode != stagingv1.FailureSizeMismatch {
			t.Fatalf("overflow: %+v %v", result, err)
		}
	})
	t.Run("short", func(t *testing.T) {
		fixture := newManagerFixture(t, IntegrityAdmission{})
		request := beginRequest([]byte("abcd"))
		created, _ := fixture.manager.Begin(context.Background(), testScope(), "upload-1", request)
		written, _ := fixture.manager.Stream(context.Background(), testScope(), created.Upload.ID, strings.NewReader("abc"))
		result, err := fixture.manager.Complete(context.Background(), testScope(), stagingv1.UploadRevisionRequest{UploadID: created.Upload.ID, ExpectedRevision: written.Upload.Revision})
		if err != nil || result.Upload.State != stagingv1.StateRejected || result.FailureCode != stagingv1.FailureSizeMismatch {
			t.Fatalf("short: %+v %v", result, err)
		}
	})
	t.Run("digest", func(t *testing.T) {
		fixture := newManagerFixture(t, IntegrityAdmission{})
		request := beginRequest([]byte("good"))
		created, _ := fixture.manager.Begin(context.Background(), testScope(), "upload-1", request)
		written, _ := fixture.manager.Stream(context.Background(), testScope(), created.Upload.ID, strings.NewReader("evil"))
		result, err := fixture.manager.Complete(context.Background(), testScope(), stagingv1.UploadRevisionRequest{UploadID: created.Upload.ID, ExpectedRevision: written.Upload.Revision})
		if err != nil || result.Upload.State != stagingv1.StateRejected || result.FailureCode != stagingv1.FailureDigestMismatch {
			t.Fatalf("digest: %+v %v", result, err)
		}
	})
	t.Run("admission", func(t *testing.T) {
		fixture := newManagerFixture(t, rejectingAdmission{})
		content := []byte("candidate")
		created, _ := fixture.manager.Begin(context.Background(), testScope(), "upload-1", beginRequest(content))
		written, _ := fixture.manager.Stream(context.Background(), testScope(), created.Upload.ID, bytes.NewReader(content))
		result, err := fixture.manager.Complete(context.Background(), testScope(), stagingv1.UploadRevisionRequest{UploadID: created.Upload.ID, ExpectedRevision: written.Upload.Revision})
		if err != nil || result.Upload.State != stagingv1.StateRejected || result.FailureCode != stagingv1.FailureAdmissionRejected {
			t.Fatalf("admission: %+v %v", result, err)
		}
	})
}

func TestManagerEnforcesOwnershipCASAndCapacity(t *testing.T) {
	fixture := newManagerFixture(t, IntegrityAdmission{})
	request := beginRequest(make([]byte, 100))
	first, err := fixture.manager.Begin(context.Background(), testScope(), "upload-1", request)
	if err != nil {
		t.Fatal(err)
	}
	request.Path = "assets/second.bin"
	second, err := fixture.manager.Begin(context.Background(), testScope(), "upload-2", request)
	if err != nil {
		t.Fatal(err)
	}
	request.Path = "assets/third.bin"
	if _, err := fixture.manager.Begin(context.Background(), testScope(), "upload-3", request); errorCode(err) != stagingv1.ErrorLimitExceeded {
		t.Fatalf("第三个并发 Upload 应被拒绝: %v", err)
	}
	other := Scope{TenantID: testScope().TenantID, ActorID: "user:bob"}
	if _, err := fixture.manager.Status(context.Background(), other, stagingv1.UploadStatusRequest{UploadID: first.Upload.ID}); errorCode(err) != stagingv1.ErrorLeaseNotFound {
		t.Fatalf("跨主体读取未隐藏: %v", err)
	}
	if _, err := fixture.manager.Renew(context.Background(), testScope(), stagingv1.RenewUploadRequest{UploadID: first.Upload.ID, ExpectedRevision: 99, LeaseSeconds: 60}); errorCode(err) != stagingv1.ErrorLeaseConflict {
		t.Fatalf("错误 revision 未冲突: %v", err)
	}
	aborted, err := fixture.manager.Abort(context.Background(), testScope(), stagingv1.UploadRevisionRequest{UploadID: second.Upload.ID, ExpectedRevision: second.Upload.Revision})
	if err != nil || aborted.Upload.State != stagingv1.StateAborted {
		t.Fatalf("abort: %+v %v", aborted, err)
	}
	if _, err := fixture.manager.Begin(context.Background(), testScope(), "upload-3", request); err != nil {
		t.Fatalf("终止后应释放活动并发配额: %v", err)
	}
}

func TestManagerExpiresReclaimsAndDeletesTerminalState(t *testing.T) {
	fixture := newManagerFixture(t, IntegrityAdmission{})
	content := []byte("temporary")
	created, _ := fixture.manager.Begin(context.Background(), testScope(), "upload-1", beginRequest(content))
	written, _ := fixture.manager.Stream(context.Background(), testScope(), created.Upload.ID, bytes.NewReader(content))
	ready, err := fixture.manager.Complete(context.Background(), testScope(), stagingv1.UploadRevisionRequest{UploadID: created.Upload.ID, ExpectedRevision: written.Upload.Revision})
	if err != nil {
		t.Fatal(err)
	}
	object := filepath.Join(fixture.root, "tenants", pathDigest(testScope().TenantID), "objects", "sha256", ready.Content.Digest)
	fixture.now = fixture.now.Add(61 * time.Second)
	if count, err := fixture.manager.Reclaim(context.Background()); err != nil || count != 1 {
		t.Fatalf("expire reclaim: count=%d err=%v", count, err)
	}
	if _, err := os.Stat(object); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("无引用临时对象未回收: %v", err)
	}
	status, err := fixture.manager.Status(context.Background(), testScope(), stagingv1.UploadStatusRequest{UploadID: created.Upload.ID})
	if err != nil || status.Upload.State != stagingv1.StateExpired {
		t.Fatalf("expired status: %+v %v", status, err)
	}
	fixture.now = fixture.now.Add(time.Hour)
	if _, err := fixture.manager.Reclaim(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.Status(context.Background(), testScope(), stagingv1.UploadStatusRequest{UploadID: created.Upload.ID}); errorCode(err) != stagingv1.ErrorLeaseNotFound {
		t.Fatalf("终态记录未删除: %v", err)
	}
}

func TestReclaimKeepsCASObjectWhileAnotherReadyLeaseProtectsIt(t *testing.T) {
	fixture := newManagerFixture(t, IntegrityAdmission{})
	content := []byte("shared")
	request := beginRequest(content)
	first, _ := fixture.manager.Begin(context.Background(), testScope(), "upload-1", request)
	request.Path = "assets/shared-copy.bin"
	second, _ := fixture.manager.Begin(context.Background(), testScope(), "upload-2", request)
	for _, upload := range []stagingv1.UploadStatusResult{first, second} {
		written, err := fixture.manager.Stream(context.Background(), testScope(), upload.Upload.ID, bytes.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.manager.Complete(context.Background(), testScope(), stagingv1.UploadRevisionRequest{UploadID: upload.Upload.ID, ExpectedRevision: written.Upload.Revision}); err != nil {
			t.Fatal(err)
		}
	}
	secondStatus, _ := fixture.manager.Status(context.Background(), testScope(), stagingv1.UploadStatusRequest{UploadID: second.Upload.ID})
	secondStatus, err := fixture.manager.Renew(context.Background(), testScope(), stagingv1.RenewUploadRequest{UploadID: second.Upload.ID, ExpectedRevision: secondStatus.Upload.Revision, LeaseSeconds: 120})
	if err != nil {
		t.Fatal(err)
	}
	object := filepath.Join(fixture.root, "tenants", pathDigest(testScope().TenantID), "objects", "sha256", contentDigest(content))
	fixture.now = fixture.now.Add(61 * time.Second)
	if _, err := fixture.manager.Reclaim(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(object); err != nil {
		t.Fatalf("第二个 Ready Lease 仍有效时对象被删除: %v", err)
	}
	fixture.now = secondStatus.Upload.LeaseExpiresAt.Add(time.Second)
	if _, err := fixture.manager.Reclaim(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(object); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("最后一个 Ready Lease 过期后对象未删除: %v", err)
	}
}

func TestFileProviderRejectsSymlinkedStorageDirectory(t *testing.T) {
	fixture := newManagerFixture(t, IntegrityAdmission{})
	content := []byte("safe")
	created, err := fixture.manager.Begin(context.Background(), testScope(), "upload-1", beginRequest(content))
	if err != nil {
		t.Fatal(err)
	}
	tenantRoot := filepath.Join(fixture.root, "tenants", pathDigest(testScope().TenantID))
	staging := filepath.Join(tenantRoot, "staging")
	if err := os.Mkdir(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(staging); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), staging); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.Stream(context.Background(), testScope(), created.Upload.ID, bytes.NewReader(content)); errorCode(err) != stagingv1.ErrorStorageUnavailable {
		t.Fatalf("符号链接目录未失败关闭: %v", err)
	}
}

func errorCode(err error) string {
	code, _ := ErrorDetails(err)
	return code
}
