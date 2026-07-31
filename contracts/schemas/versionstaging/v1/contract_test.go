package versionstagingv1_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	versionresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
)

func TestUploadLeaseBindsWorkspaceResourceAndExactContent(t *testing.T) {
	request := validBeginRequest()
	if err := stagingv1.ValidateBeginUploadRequest(request); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	result := stagingv1.UploadStatusResult{Upload: stagingv1.UploadLease{
		Protocol: stagingv1.Protocol, ID: "stg_1234567890abcdef", SessionID: request.SessionID,
		EnvironmentDigest: request.EnvironmentDigest, Resource: request.Resource, Path: request.Path,
		MediaType: request.MediaType, ExpectedDigest: request.ExpectedDigest, ExpectedSize: request.ExpectedSize,
		ReceivedSize: request.ExpectedSize, State: stagingv1.StateReady, Revision: 3,
		CreatedAt: now, UpdatedAt: now.Add(time.Minute), LeaseExpiresAt: now.Add(time.Hour),
	}, Content: &stagingv1.ContentDescriptor{Digest: request.ExpectedDigest, Size: request.ExpectedSize, MediaType: request.MediaType}}
	if err := stagingv1.ValidateUploadStatusResult(result); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	if parsed, err := stagingv1.ParseResult(stagingv1.OperationUploadStatus, raw); err != nil || parsed.Content == nil || parsed.Content.Digest != request.ExpectedDigest {
		t.Fatalf("解析 Staging 结果失败: %+v err=%v", parsed, err)
	}
	for _, forbidden := range []string{"provider", "endpoint", "credential", "token", "uploadUrl", "localPath"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("Staging 控制面泄漏了 %s: %s", forbidden, raw)
		}
	}
}

func TestUploadValidationFailsClosedOnPathMediaAndAdmissionMismatch(t *testing.T) {
	request := validBeginRequest()
	request.Path = "../escape"
	if err := stagingv1.ValidateBeginUploadRequest(request); err == nil {
		t.Fatal("Staging 必须拒绝路径穿越")
	}
	request = validBeginRequest()
	request.MediaType = "Application/Octet-Stream"
	if err := stagingv1.ValidateBeginUploadRequest(request); err == nil {
		t.Fatal("Staging 必须拒绝非规范 mediaType")
	}

	now := time.Now().UTC()
	upload := stagingv1.UploadLease{
		Protocol: stagingv1.Protocol, ID: "stg_1234567890abcdef", SessionID: "ws_1234567890abcdef",
		EnvironmentDigest: strings.Repeat("a", 64), Resource: versionresourcev1.ResourceKey{Type: "script.bundle", ID: "daily"},
		Path: "payload.bin", MediaType: "application/octet-stream", ExpectedDigest: strings.Repeat("b", 64),
		ExpectedSize: 12, ReceivedSize: 12, State: stagingv1.StateReady, Revision: 2,
		CreatedAt: now, UpdatedAt: now, LeaseExpiresAt: now.Add(time.Hour),
	}
	mismatched := stagingv1.UploadStatusResult{Upload: upload, Content: &stagingv1.ContentDescriptor{Digest: strings.Repeat("c", 64), Size: 12, MediaType: upload.MediaType}}
	if err := stagingv1.ValidateUploadStatusResult(mismatched); err == nil {
		t.Fatal("Ready 内容必须与 Lease 的预期摘要完全一致")
	}
	upload.State = stagingv1.StateRejected
	rejected := stagingv1.UploadStatusResult{Upload: upload, FailureCode: stagingv1.FailureAdmissionRejected}
	if err := stagingv1.ValidateUploadStatusResult(rejected); err != nil {
		t.Fatal(err)
	}
	rejected.FailureCode = "scanner_detail"
	if err := stagingv1.ValidateUploadStatusResult(rejected); err == nil {
		t.Fatal("拒绝结果不得暴露任意扫描器细节")
	}
}

func TestStagingWireIsStrictAndHasStableErrorCatalog(t *testing.T) {
	request := validBeginRequest()
	raw, _ := json.Marshal(request)
	parsed, err := stagingv1.ParseRequest(stagingv1.OperationBeginUpload, raw)
	if err != nil || parsed.(*stagingv1.BeginUploadRequest).ExpectedDigest != request.ExpectedDigest {
		t.Fatalf("解析 Staging 请求失败: %+v err=%v", parsed, err)
	}
	if _, err := stagingv1.ParseRequest(stagingv1.OperationBeginUpload, append(raw[:len(raw)-1], []byte(`,"provider":"local"}`)...)); err == nil {
		t.Fatal("Staging Wire 必须拒绝未知 Provider 字段")
	}
	if !stagingv1.KnownErrorCode(stagingv1.ErrorStorageUnavailable) || stagingv1.KnownErrorCode("arbitrary") {
		t.Fatal("Staging 稳定错误码目录无效")
	}
}

func validBeginRequest() stagingv1.BeginUploadRequest {
	return stagingv1.BeginUploadRequest{
		SessionID: "ws_1234567890abcdef", ExpectedSessionRevision: 2,
		EnvironmentDigest: strings.Repeat("a", 64), Resource: versionresourcev1.ResourceKey{Type: "script.bundle", ID: "daily"},
		Path: "payload.bin", MediaType: "application/octet-stream", ExpectedDigest: strings.Repeat("b", 64),
		ExpectedSize: 12, LeaseSeconds: 300,
	}
}
