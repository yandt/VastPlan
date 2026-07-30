package versionledger_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	versionledgersdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/versionledger"
)

type ledgerHost struct {
	call func(*contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)
}

func (h ledgerHost) Call(_ context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	return h.call(target, call, payload)
}

var _ sdk.Host = ledgerHost{}

func TestClientUsesCapabilityAndValidatesBothDirections(t *testing.T) {
	stream := versioningv1.StreamKey{Namespace: "portal.configuration", StreamID: "admin"}
	host := ledgerHost{call: func(target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if target.GetCapability() != versioningv1.LedgerCapability || target.GetOperation() != versioningv1.OperationPutVersion {
			t.Fatalf("错误调用目标: %+v", target)
		}
		parsed, err := versioningv1.ParseRequest(versioningv1.OperationPutVersion, payload)
		if err != nil {
			t.Fatal(err)
		}
		request := parsed.(*versioningv1.PutVersionRequest)
		versionID, _ := versioningv1.DeriveVersionID(call.GetTenantId(), request.Stream, request.IdempotencyKey)
		digest, _ := versioningv1.ContentDigest(request.Content)
		response, _ := json.Marshal(versioningv1.PutVersionResult{Version: versioningv1.VersionRecord{
			Protocol: versioningv1.Protocol,
			Ref:      versioningv1.VersionRef{Stream: request.Stream, VersionID: versionID, Sequence: 1, ContentDigest: digest},
			Content:  request.Content, ActorID: "plugin:consumer", CreatedAt: time.Now().UTC(),
		}})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, response, nil
	}}
	client, err := versionledgersdk.New(host)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.PutVersion(context.Background(), &contractv1.CallContext{TenantId: "tenant-a"}, versioningv1.PutVersionRequest{
		Stream: stream, IdempotencyKey: "portal-version:1", Content: json.RawMessage(`{"z":1,"a":2}`),
	})
	if err != nil || string(result.Version.Content) != `{"a":2,"z":1}` {
		t.Fatalf("客户端未执行规范请求/结果校验: %+v %v", result, err)
	}
}

func TestClientPreservesStableServiceError(t *testing.T) {
	client, err := versionledgersdk.New(ledgerHost{call: func(*contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{
			Code: versioningv1.ErrorConflict, Message: "CAS conflict", Retryable: false,
		}}, nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetHead(context.Background(), &contractv1.CallContext{TenantId: "tenant-a"}, versioningv1.GetHeadRequest{
		Stream: versioningv1.StreamKey{Namespace: "portal.configuration", StreamID: "admin"}, Name: "draft",
	})
	if !versionledgersdk.IsCode(err, versioningv1.ErrorConflict) {
		t.Fatalf("稳定错误码丢失: %v", err)
	}
	var serviceErr *versionledgersdk.ServiceError
	if !errors.As(err, &serviceErr) || serviceErr.Retryable {
		t.Fatalf("错误可重试属性不一致: %+v", serviceErr)
	}
}
