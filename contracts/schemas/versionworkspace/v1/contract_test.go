package versionworkspacev1_test

import (
	"strings"
	"testing"
	"time"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

func TestOpenRequestDefaultsToProfileWithoutExposingProvider(t *testing.T) {
	if !workspacev1.KnownErrorCode(workspacev1.ErrorLeaseExpired) || workspacev1.KnownErrorCode("arbitrary") {
		t.Fatal("Workspace 稳定错误码目录无效")
	}
	request := workspacev1.OpenRequest{
		EnvironmentID: "platform-development", Resource: resourcev1.ResourceKey{Type: "portal.configuration", ID: "admin"}, ReadOnly: false, BaseHead: "draft", TargetHead: "draft",
	}
	if err := workspacev1.ValidateOpenRequest(request); err != nil {
		t.Fatal(err)
	}
	request.ReadOnly = true
	if err := workspacev1.ValidateOpenRequest(request); err == nil {
		t.Fatal("只读工作区不得更新 Head")
	}
}

func TestSessionBindsExactEnvironmentRevisionAndLease(t *testing.T) {
	now := time.Now().UTC()
	base := versioningv1.VersionRef{Stream: versioningv1.StreamKey{Namespace: "portal.configuration", StreamID: "admin"}, VersionID: strings.Repeat("b", 64), Sequence: 1, ContentDigest: strings.Repeat("c", 64)}
	session := workspacev1.Session{
		Protocol: workspacev1.Protocol, ID: "ws_1234567890abcdef", EnvironmentID: "platform-development",
		EnvironmentDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Resource:          resourcev1.ResourceKey{Type: "portal.configuration", ID: "admin"}, Namespace: "portal.configuration",
		Adapter: "version.resource.json.v1", Mode: resourcev1.ModeSnapshot, BaseRef: &base, BaseHead: "draft", TargetHead: "draft", State: workspacev1.StateClean,
		Revision: 1, CreatedAt: now, LeaseExpiresAt: now.Add(time.Hour),
	}
	if err := workspacev1.ValidateSession(session); err != nil {
		t.Fatal(err)
	}
	session.EnvironmentDigest = "mutable-profile"
	if err := workspacev1.ValidateSession(session); err == nil {
		t.Fatal("Session 必须绑定精确 Environment digest")
	}
}

func TestChangesMustBeDeterministicAndConsistent(t *testing.T) {
	now := time.Now().UTC()
	result := workspacev1.ChangesResult{
		Session: workspacev1.Session{
			Protocol: workspacev1.Protocol, ID: "ws_1234567890abcdef", EnvironmentID: "platform-development",
			EnvironmentDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Resource:          resourcev1.ResourceKey{Type: "script.bundle", ID: "daily"}, Namespace: "script.bundle", Adapter: "version.resource.files.v1",
			Mode: resourcev1.ModeOverlay, State: workspacev1.StateDirty, Revision: 2, CreatedAt: now, LeaseExpiresAt: now.Add(time.Hour),
		},
		Dirty: true, DiffAvailable: true, ChangedPaths: []string{"README.md", "src/main.ts"}, Summary: workspacev1.ChangeSummary{Added: 1, Modified: 1, Total: 2},
	}
	if err := workspacev1.ValidateChangesResult(result); err != nil {
		t.Fatal(err)
	}
	result.ChangedPaths[0], result.ChangedPaths[1] = result.ChangedPaths[1], result.ChangedPaths[0]
	if err := workspacev1.ValidateChangesResult(result); err == nil {
		t.Fatal("变更路径必须确定性排序")
	}
}

func TestResourceDescriptionAndDigestOnlyChanges(t *testing.T) {
	resource := resourcev1.ResourceKey{Type: "archive.bundle", ID: "daily"}
	request := workspacev1.DescribeResourceRequest{EnvironmentID: "platform-development", Resource: resource, RequestedMode: resourcev1.ModeOverlay}
	if err := workspacev1.ValidateDescribeResourceRequest(request); err != nil {
		t.Fatal(err)
	}
	description := workspacev1.ResourceDescription{
		Resolution: workspacev1.ResourceResolution{
			EnvironmentID: request.EnvironmentID, EnvironmentDigest: strings.Repeat("a", 64), Resource: resource,
			Namespace: "archive.bundle", Adapter: "version.resource.blob.v1", Mode: resourcev1.ModeOverlay,
		},
		ContentKind: resourcev1.ContentFiles, AllowedModes: []string{resourcev1.ModeOverlay}, DefaultMode: resourcev1.ModeOverlay,
		MaxBytes: 64 << 20, SecretPolicy: resourcev1.SecretPolicyForbidden,
		Capabilities: resourcev1.AdapterCapabilities{Normalize: true},
	}
	if err := workspacev1.ValidateResourceDescription(description); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	changes := workspacev1.ChangesResult{
		Session: workspacev1.Session{
			Protocol: workspacev1.Protocol, ID: "ws_1234567890abcdef", EnvironmentID: request.EnvironmentID, EnvironmentDigest: strings.Repeat("a", 64),
			Resource: resource, Namespace: "archive.bundle", Adapter: "version.resource.blob.v1", Mode: resourcev1.ModeOverlay,
			State: workspacev1.StateDirty, Revision: 2, CreatedAt: now, LeaseExpiresAt: now.Add(time.Hour),
		},
		Dirty: true, DiffAvailable: false,
	}
	if err := workspacev1.ValidateChangesResult(changes); err != nil {
		t.Fatal(err)
	}
	changes.ChangedPaths = []string{"payload.bin"}
	if err := workspacev1.ValidateChangesResult(changes); err == nil {
		t.Fatal("diffAvailable=false 时不得伪造详细变化")
	}
}

func TestCommitRequiresStableOperationIdentity(t *testing.T) {
	request := workspacev1.CommitRequest{SessionID: "ws_1234567890abcdef", ExpectedRevision: 2, OperationID: "portal-publication:0001"}
	if err := workspacev1.ValidateCommitRequest(request); err != nil {
		t.Fatal(err)
	}
	request.OperationID = "short"
	if err := workspacev1.ValidateCommitRequest(request); err == nil {
		t.Fatal("commit 必须携带可持久化的稳定 operationId")
	}
}

func TestContentUploadRequestsBindWorkspaceRevisionWithoutCarryingProvider(t *testing.T) {
	request := workspacev1.BeginContentUploadRequest{
		SessionID: "ws_1234567890abcdef", ExpectedRevision: 2, Path: "src/main.ts", MediaType: "text/typescript",
		ExpectedDigest: strings.Repeat("a", 64), ExpectedSize: 128, LeaseSeconds: 300,
	}
	if err := workspacev1.ValidateBeginContentUploadRequest(request); err != nil {
		t.Fatal(err)
	}
	request.Path = "../secret"
	if err := workspacev1.ValidateBeginContentUploadRequest(request); err == nil {
		t.Fatal("Workspace 上传路径不得逃逸资源根")
	}
	if err := workspacev1.ValidateRenewContentUploadRequest(workspacev1.RenewContentUploadRequest{
		SessionID: "ws_1234567890abcdef", UploadID: "stg_1234567890abcdef", ExpectedUploadRevision: 1, LeaseSeconds: 300,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCommittedRequestsAndResultsBindOneResourceStream(t *testing.T) {
	stream := versioningv1.StreamKey{Namespace: "portal.configuration", StreamID: "admin"}
	left := versioningv1.VersionRef{Stream: stream, VersionID: strings.Repeat("a", 64), Sequence: 1, ContentDigest: strings.Repeat("b", 64)}
	right := versioningv1.VersionRef{Stream: stream, VersionID: strings.Repeat("c", 64), Sequence: 2, ContentDigest: strings.Repeat("d", 64)}
	resource := resourcev1.ResourceKey{Type: "portal.configuration", ID: "admin"}
	environmentDigest := strings.Repeat("e", 64)
	if err := workspacev1.ValidateCommittedRequest(workspacev1.CommittedRequest{EnvironmentID: "platform-development", EnvironmentDigest: environmentDigest, Resource: resource, Ref: left}); err != nil {
		t.Fatal(err)
	}
	if err := workspacev1.ValidateCompareCommittedRequest(workspacev1.CompareCommittedRequest{EnvironmentID: "platform-development", EnvironmentDigest: environmentDigest, Resource: resource, Left: left, Right: right}); err != nil {
		t.Fatal(err)
	}
	resolution := workspacev1.ResourceResolution{
		EnvironmentID: "platform-development", EnvironmentDigest: environmentDigest, Resource: resource,
		Namespace: stream.Namespace, Adapter: "version.resource.json.v1", Mode: resourcev1.ModeSnapshot,
	}
	result := workspacev1.CompareCommittedResult{
		Resolution: resolution, Left: left, Right: right, LeftDigest: left.ContentDigest, RightDigest: right.ContentDigest,
		Dirty: true, DiffAvailable: true, ChangedPaths: []string{"/name"}, Summary: workspacev1.ChangeSummary{Modified: 1, Total: 1},
	}
	if err := workspacev1.ValidateCompareCommittedResult(result); err != nil {
		t.Fatal(err)
	}
	result.Resolution.Resource.ID = "other"
	if err := workspacev1.ValidateCompareCommittedResult(result); err == nil {
		t.Fatal("比较结果不得跨越 Resource stream")
	}
}
