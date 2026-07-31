package versionstaging_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	stagingsdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/versionstaging"
)

type stagingHost struct {
	call func(*contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)
}

func (h stagingHost) Call(_ context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	return h.call(target, call, payload)
}

var _ sdk.Host = stagingHost{}

func TestClientUsesNeutralCapabilityAndStrictWire(t *testing.T) {
	request := stagingv1.BeginUploadRequest{
		SessionID: "ws_1234567890abcdef", ExpectedSessionRevision: 1, EnvironmentDigest: strings.Repeat("a", 64),
		Resource: resourcev1.ResourceKey{Type: "script.bundle", ID: "daily"}, Path: "main.ts", MediaType: "text/typescript",
		ExpectedDigest: strings.Repeat("b", 64), ExpectedSize: 128, LeaseSeconds: 300,
	}
	client, err := stagingsdk.New(stagingHost{call: func(target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if target.GetCapability() != stagingv1.Capability || target.GetOperation() != stagingv1.OperationBeginUpload {
			t.Fatalf("错误调用目标: %+v", target)
		}
		parsed, parseErr := stagingv1.ParseRequest(stagingv1.OperationBeginUpload, payload)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		input := parsed.(*stagingv1.BeginUploadRequest)
		now := time.Now().UTC()
		response, _ := json.Marshal(stagingv1.UploadStatusResult{Upload: stagingv1.UploadLease{
			Protocol: stagingv1.Protocol, ID: "stg_1234567890abcdef", SessionID: input.SessionID, EnvironmentDigest: input.EnvironmentDigest,
			Resource: input.Resource, Path: input.Path, MediaType: input.MediaType, ExpectedDigest: input.ExpectedDigest, ExpectedSize: input.ExpectedSize,
			State: stagingv1.StatePending, Revision: 1, CreatedAt: now, UpdatedAt: now, LeaseExpiresAt: now.Add(5 * time.Minute),
		}})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, response, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.BeginUpload(context.Background(), &contractv1.CallContext{TenantId: "tenant-a"}, request)
	if err != nil || result.Upload.Path != request.Path {
		t.Fatalf("BeginUpload: %+v %v", result, err)
	}
}
