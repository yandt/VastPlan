package main

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
	target *contractv1.CallTarget
}

func (h *scopedHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	h.target = target
	if string(payload) != "{}" {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "test.payload"}}, nil, nil
	}
	values := json.RawMessage(`{"greetingTemplate":"Welcome, {{name}}"}`)
	digest, _ := configurationscopedv1.DigestValues(values)
	raw, _ := json.Marshal(configurationscopedv1.Resolution{
		Protocol: configurationscopedv1.Protocol, ConfigurationID: "cfg_" + strings.Repeat("a", 24), Scope: configurationscopedv1.ScopeTenant,
		Revision: 1, Digest: digest, SchemaDigest: strings.Repeat("b", 64), ArtifactSHA256: strings.Repeat("c", 64),
		Values: values, Source: "active", ObservedAt: time.Now().UTC(),
	})
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func TestGreetResolvesTenantConfigurationWithoutCallerSelectedIdentity(t *testing.T) {
	host := &scopedHost{}
	call := &contractv1.CallContext{
		TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "alice"},
		Principal: &contractv1.Principal{UserId: "alice", Username: "Alice"}, Trace: &contractv1.Trace{TraceId: "trace-a"},
	}
	result, raw, err := greet(context.Background(), host, call, []byte(`{"name":"Lin"}`))
	if err != nil || result.Status != contractv1.CallResult_STATUS_OK {
		t.Fatalf("greet: result=%+v err=%v", result, err)
	}
	var response map[string]any
	_ = json.Unmarshal(raw, &response)
	if response["greeting"] != "Welcome, Lin" {
		t.Fatalf("未使用 scoped Active: %s", raw)
	}
	if host.target.GetExtensionPoint() != configurationscopedv1.ExtensionPoint || host.target.GetCapability() != configurationscopedv1.Capability || host.target.GetOperation() != configurationscopedv1.OperationResolve {
		t.Fatalf("错误 resolver target: %+v", host.target)
	}
}
