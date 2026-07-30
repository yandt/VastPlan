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
