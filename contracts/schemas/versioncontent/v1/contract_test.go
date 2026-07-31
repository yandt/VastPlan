package versioncontentv1_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	contentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioncontent/v1"
	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
)

func TestProtectionContractBindsManifestAndExactVersion(t *testing.T) {
	request := validPrepareRequest()
	if err := contentv1.ValidatePrepareRequest(request); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	expires := now.Add(time.Hour)
	protection := contentv1.Protection{
		Protocol: contentv1.Protocol, ID: "vcr_1234567890abcdef", OperationID: request.OperationID,
		EnvironmentDigest: request.EnvironmentDigest, Resource: request.Resource, Stream: request.Stream,
		ManifestDigest: request.ManifestDigest, Entries: []contentv1.ContentEntry{{Path: "main.bin", Digest: strings.Repeat("b", 64), Size: 12, MediaType: "application/octet-stream"}},
		State: contentv1.StatePrepared, Revision: 1, CreatedAt: now, UpdatedAt: now, ExpiresAt: &expires,
	}
	if err := contentv1.ValidateProtection(protection); err != nil {
		t.Fatal(err)
	}
	version := versioningv1.VersionRef{Stream: request.Stream, VersionID: strings.Repeat("c", 64), Sequence: 1, ContentDigest: request.ManifestDigest}
	protection.State, protection.Revision, protection.Version, protection.ExpiresAt = contentv1.StateConfirmed, 2, &version, nil
	if err := contentv1.ValidateProtection(protection); err != nil {
		t.Fatal(err)
	}
	version.ContentDigest = strings.Repeat("d", 64)
	protection.Version = &version
	if err := contentv1.ValidateProtection(protection); err == nil {
		t.Fatal("VersionRef 必须与准备的 manifest 摘要一致")
	}
}

func TestProtectionWireRejectsUnknownAndUnsortedEntries(t *testing.T) {
	request := validPrepareRequest()
	raw, _ := json.Marshal(request)
	if _, err := contentv1.ParseRequest(contentv1.OperationPrepare, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := contentv1.ParseRequest(contentv1.OperationPrepare, append(raw[:len(raw)-1], []byte(`,"provider":"local"}`)...)); err == nil {
		t.Fatal("Content Reference Wire 必须拒绝 Provider 泄漏字段")
	}
	request.Entries = append([]contentv1.ContentEntry{{Path: "z.bin", Digest: strings.Repeat("d", 64), Size: 1, MediaType: "application/octet-stream"}}, request.Entries...)
	if err := contentv1.ValidatePrepareRequest(request); err == nil {
		t.Fatal("内容条目必须按 path 严格排序")
	}
}

func validPrepareRequest() contentv1.PrepareRequest {
	return contentv1.PrepareRequest{
		OperationID: "workspace-commit:0001", SessionID: "ws_1234567890abcdef", ExpectedSessionRevision: 2,
		EnvironmentDigest: strings.Repeat("a", 64), Resource: resourcev1.ResourceKey{Type: "script.bundle", ID: "daily"},
		Stream: versioningv1.StreamKey{Namespace: "script.bundle", StreamID: "daily"}, ManifestDigest: strings.Repeat("e", 64),
		Entries: []contentv1.ContentEntry{{Path: "main.bin", UploadID: "stg_1234567890abcdef", Digest: strings.Repeat("b", 64), Size: 12, MediaType: "application/octet-stream"}},
	}
}
