package versionworkspace_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	workspace "cdsoft.com.cn/VastPlan/extensions/sdk/go/versionworkspace"
)

type workspaceHost struct {
	calls int
	call  func(*contractv1.CallTarget, []byte) (*contractv1.CallResult, []byte, error)
}

func (h *workspaceHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	h.calls++
	return h.call(target, payload)
}

var _ sdk.Host = (*workspaceHost)(nil)

func TestClientValidatesRequestBeforeCallingHostAndResponseAfter(t *testing.T) {
	now := time.Now().UTC()
	base := versioningv1.VersionRef{Stream: versioningv1.StreamKey{Namespace: "portal.configuration", StreamID: "admin"}, VersionID: strings.Repeat("b", 64), Sequence: 1, ContentDigest: strings.Repeat("c", 64)}
	host := &workspaceHost{call: func(target *contractv1.CallTarget, payload []byte) (*contractv1.CallResult, []byte, error) {
		if target.GetCapability() != workspacev1.Capability || target.GetOperation() != workspacev1.OperationOpen {
			t.Fatalf("错误 Workspace 目标: %+v", target)
		}
		if _, err := workspacev1.ParseRequest(workspacev1.OperationOpen, payload); err != nil {
			t.Fatal(err)
		}
		response, _ := json.Marshal(workspacev1.SessionResult{Session: workspacev1.Session{
			Protocol: workspacev1.Protocol, ID: "ws_1234567890abcdef", EnvironmentID: "platform-development",
			EnvironmentDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Resource:          resourcev1.ResourceKey{Type: "portal.configuration", ID: "admin"}, Namespace: "portal.configuration",
			Adapter: "version.resource.json.v1", Mode: resourcev1.ModeSnapshot, BaseRef: &base, BaseHead: "draft", TargetHead: "draft", State: workspacev1.StateClean,
			Revision: 1, CreatedAt: now, LeaseExpiresAt: now.Add(time.Hour),
		}})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, response, nil
	}}
	client, err := workspace.New(host)
	if err != nil {
		t.Fatal(err)
	}
	request := workspacev1.OpenRequest{EnvironmentID: "platform-development", Resource: resourcev1.ResourceKey{Type: "portal.configuration", ID: "admin"}, BaseHead: "draft", TargetHead: "draft"}
	session, err := client.Open(context.Background(), &contractv1.CallContext{TenantId: "tenant-a"}, request)
	if err != nil || session.Mode != resourcev1.ModeSnapshot || host.calls != 1 {
		t.Fatalf("Workspace client 调用失败: %+v %v calls=%d", session, err, host.calls)
	}
	request.Resource.ID = "../escape"
	if _, err := client.Open(context.Background(), &contractv1.CallContext{TenantId: "tenant-a"}, request); err == nil || host.calls != 1 {
		t.Fatalf("无效请求不得进入宿主: err=%v calls=%d", err, host.calls)
	}
}

func TestClientReadsAndComparesCommittedVersions(t *testing.T) {
	now := time.Now().UTC()
	stream := versioningv1.StreamKey{Namespace: "portal.configuration", StreamID: "admin"}
	leftContent, rightContent := json.RawMessage(`{"name":"left"}`), json.RawMessage(`{"name":"right"}`)
	leftDigest, _ := versioningv1.ContentDigest(leftContent)
	rightDigest, _ := versioningv1.ContentDigest(rightContent)
	left := versioningv1.VersionRecord{
		Protocol: versioningv1.Protocol, Ref: versioningv1.VersionRef{Stream: stream, VersionID: strings.Repeat("a", 64), Sequence: 1, ContentDigest: leftDigest},
		Content: leftContent, ActorID: "plugin:portal", CreatedAt: now,
	}
	right := versioningv1.VersionRecord{
		Protocol: versioningv1.Protocol, Ref: versioningv1.VersionRef{Stream: stream, VersionID: strings.Repeat("b", 64), Sequence: 2, ContentDigest: rightDigest},
		Parents: []versioningv1.VersionRef{left.Ref}, Content: rightContent, ActorID: "plugin:portal", CreatedAt: now.Add(time.Second),
	}
	resolution := workspacev1.ResourceResolution{
		EnvironmentID: "platform-development", EnvironmentDigest: strings.Repeat("c", 64),
		Resource: resourcev1.ResourceKey{Type: "portal.configuration", ID: "admin"}, Namespace: stream.Namespace,
		Adapter: "version.resource.json.v1", Mode: resourcev1.ModeSnapshot,
	}
	host := &workspaceHost{call: func(target *contractv1.CallTarget, payload []byte) (*contractv1.CallResult, []byte, error) {
		if _, err := workspacev1.ParseRequest(target.GetOperation(), payload); err != nil {
			t.Fatal(err)
		}
		var response any
		switch target.GetOperation() {
		case workspacev1.OperationReadCommitted:
			response = workspacev1.CommittedSnapshotResult{
				Resolution: resolution, Version: left,
				Snapshot: resourcev1.Snapshot{Kind: resourcev1.ContentJSON, MediaType: "application/json", JSON: leftContent}, Digest: leftDigest,
			}
		case workspacev1.OperationCompareCommitted:
			response = workspacev1.CompareCommittedResult{
				Resolution: resolution, Left: left.Ref, Right: right.Ref, LeftDigest: leftDigest, RightDigest: rightDigest,
				Dirty: true, DiffAvailable: true, ChangedPaths: []string{"/name"}, Summary: workspacev1.ChangeSummary{Modified: 1, Total: 1},
			}
		default:
			t.Fatalf("错误 Workspace committed 目标: %+v", target)
		}
		raw, _ := json.Marshal(response)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}}
	client, err := workspace.New(host)
	if err != nil {
		t.Fatal(err)
	}
	call := &contractv1.CallContext{TenantId: "tenant-a"}
	read, err := client.ReadCommitted(context.Background(), call, workspacev1.CommittedRequest{EnvironmentID: resolution.EnvironmentID, EnvironmentDigest: resolution.EnvironmentDigest, Resource: resolution.Resource, Ref: left.Ref})
	if err != nil || read.Version.Ref != left.Ref {
		t.Fatalf("ReadCommitted SDK 失败: %+v err=%v", read, err)
	}
	compared, err := client.CompareCommitted(context.Background(), call, workspacev1.CompareCommittedRequest{EnvironmentID: resolution.EnvironmentID, EnvironmentDigest: resolution.EnvironmentDigest, Resource: resolution.Resource, Left: left.Ref, Right: right.Ref})
	if err != nil || !compared.Dirty || host.calls != 2 {
		t.Fatalf("CompareCommitted SDK 失败: %+v err=%v calls=%d", compared, err, host.calls)
	}
}

func TestClientDescribesResourceCapabilities(t *testing.T) {
	request := workspacev1.DescribeResourceRequest{
		EnvironmentID: "platform-development", Resource: resourcev1.ResourceKey{Type: "portal.configuration", ID: "admin"},
	}
	description := workspacev1.ResourceDescription{
		Resolution: workspacev1.ResourceResolution{
			EnvironmentID: request.EnvironmentID, EnvironmentDigest: strings.Repeat("d", 64), Resource: request.Resource,
			Namespace: "portal.configuration", Adapter: "version.resource.json.v1", Mode: resourcev1.ModeSnapshot,
		},
		ContentKind: resourcev1.ContentJSON, AllowedModes: []string{resourcev1.ModeSnapshot}, DefaultMode: resourcev1.ModeSnapshot,
		MaxBytes: 1 << 20, SecretPolicy: resourcev1.SecretPolicyCredentialRefsOnly,
		Capabilities: resourcev1.AdapterCapabilities{Normalize: true, Diff: true},
	}
	host := &workspaceHost{call: func(target *contractv1.CallTarget, payload []byte) (*contractv1.CallResult, []byte, error) {
		if target.GetOperation() != workspacev1.OperationDescribeResource {
			t.Fatalf("错误 Workspace describe 目标: %+v", target)
		}
		if _, err := workspacev1.ParseRequest(target.GetOperation(), payload); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(description)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}}
	client, err := workspace.New(host)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.DescribeResource(context.Background(), &contractv1.CallContext{TenantId: "tenant-a"}, request)
	if err != nil || !result.Capabilities.Diff || result.Resolution.EnvironmentDigest != description.Resolution.EnvironmentDigest {
		t.Fatalf("DescribeResource SDK 失败: %+v err=%v", result, err)
	}
}
