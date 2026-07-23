package sharedstate

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	sharedstatev1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstate/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
)

type fakeHost struct {
	target  *contractv1.CallTarget
	payload []byte
	result  *contractv1.CallResult
	raw     []byte
}

func (h *fakeHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	h.target, h.payload = target, append([]byte(nil), payload...)
	return h.result, h.raw, nil
}

func TestClientOmitsTrustedIdentityAndParsesStrictEntry(t *testing.T) {
	entry := sharedstatev1.Entry{Protocol: sharedstatev1.Protocol, Key: "active", Value: sharedstatev1.EncodeValue([]byte(`{}`)), Revision: 1, UpdatedAt: time.Now().UTC()}
	raw, _ := json.Marshal(entry)
	host := &fakeHost{result: &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw: raw}
	client, err := New(host, "tenant", "settings")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Create(context.Background(), &contractv1.CallContext{TenantId: "trusted"}, "active", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if host.target.GetCapability() != sharedstatev1.KernelService(sharedstatev1.OperationCreate) {
		t.Fatalf("target 错误: %+v", host.target)
	}
	var request map[string]any
	_ = json.Unmarshal(host.payload, &request)
	for _, forbidden := range []string{"tenantId", "pluginId", "runtimeScope"} {
		if _, ok := request[forbidden]; ok {
			t.Fatalf("请求泄漏可信身份字段 %s", forbidden)
		}
	}
}

func TestClientExposesStableConflict(t *testing.T) {
	host := &fakeHost{result: &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "state.conflict", Message: "conflict", Retryable: true}}}
	client, _ := New(host, "service", "ledger")
	_, err := client.Update(context.Background(), &contractv1.CallContext{}, "active", []byte(`{}`), 1)
	if !IsConflict(err) {
		t.Fatalf("必须保留 conflict 语义: %v", err)
	}
}
