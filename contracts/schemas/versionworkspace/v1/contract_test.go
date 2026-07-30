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
		Dirty: true, ChangedPaths: []string{"README.md", "src/main.ts"}, Summary: workspacev1.ChangeSummary{Added: 1, Modified: 1, Total: 2},
	}
	if err := workspacev1.ValidateChangesResult(result); err != nil {
		t.Fatal(err)
	}
	result.ChangedPaths[0], result.ChangedPaths[1] = result.ChangedPaths[1], result.ChangedPaths[0]
	if err := workspacev1.ValidateChangesResult(result); err == nil {
		t.Fatal("变更路径必须确定性排序")
	}
}
