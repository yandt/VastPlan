package configurationscoped

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	configurationscopedv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationscoped/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
)

type scopedHost struct {
	target  *contractv1.CallTarget
	payload []byte
	raw     []byte
}

func (h *scopedHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	h.target, h.payload = target, append([]byte(nil), payload...)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, h.raw, nil
}

func TestResolveUsesIdentityFreeTargetAndStrictResponse(t *testing.T) {
	values := json.RawMessage(`{"greetingTemplate":"Hello, {{name}}"}`)
	digest, _ := configurationscopedv1.DigestValues(values)
	response := configurationscopedv1.Resolution{
		Protocol: configurationscopedv1.Protocol, ConfigurationID: "cfg_" + strings.Repeat("a", 24), Scope: configurationscopedv1.ScopeTenant,
		Revision: 0, Digest: digest, SchemaDigest: strings.Repeat("b", 64), ArtifactSHA256: strings.Repeat("c", 64),
		Values: values, Source: "seed", ObservedAt: time.Now().UTC(),
	}
	raw, _ := json.Marshal(response)
	host := &scopedHost{raw: raw}
	var output struct {
		GreetingTemplate string `json:"greetingTemplate"`
	}
	if _, err := Resolve(context.Background(), host, &contractv1.CallContext{TenantId: "trusted"}, &output); err != nil {
		t.Fatal(err)
	}
	if host.target.GetExtensionPoint() != configurationscopedv1.ExtensionPoint || host.target.GetCapability() != configurationscopedv1.Capability || host.target.GetOperation() != configurationscopedv1.OperationResolve || string(host.payload) != "{}" || output.GreetingTemplate == "" {
		t.Fatalf("Scoped Resolve 请求漂移: target=%+v payload=%s output=%+v", host.target, host.payload, output)
	}
	var forged map[string]any
	_ = json.Unmarshal(raw, &forged)
	forged["tenantId"] = "forged"
	host.raw, _ = json.Marshal(forged)
	if _, err := Resolve(context.Background(), host, &contractv1.CallContext{TenantId: "trusted"}, &output); err == nil {
		t.Fatal("Go SDK 必须拒绝响应中的未知身份字段")
	}
}

func TestWatchRejectsInvalidRequestBeforeHostCall(t *testing.T) {
	host := &scopedHost{}
	_, err := WatchRevision(context.Background(), host, &contractv1.CallContext{}, configurationscopedv1.WatchRevisionRequest{
		AfterDigest: strings.Repeat("x", 64), TimeoutMS: configurationscopedv1.MaxWatchTimeoutMS + 1,
	})
	if err == nil {
		t.Fatal("Go SDK 必须在 HostCall 前拒绝无效 watch 请求")
	}
	if host.target != nil {
		t.Fatal("无效 watch 请求不得发送给可信 resolver")
	}
}
