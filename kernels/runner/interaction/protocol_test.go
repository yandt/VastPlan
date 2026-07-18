package interaction

import (
	"context"
	"encoding/json"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/interactionapi"
)

type invokeStub struct {
	target *contractv1.CallTarget
	call   *contractv1.CallContext
}

func (s *invokeStub) Invoke(_ context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
	s.target, s.call = target, call
	raw, _ := json.Marshal(interactionapi.Record{})
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}
func TestProtocolBrokerBuildsRunnerBoundCallContext(t *testing.T) {
	stub := &invokeStub{}
	broker, err := NewProtocolBroker(stub)
	if err != nil {
		t.Fatal(err)
	}
	_, err = broker.Cancel(context.Background(), interactionapi.Subject{ID: "runner-a", TenantID: "tenant-a"}, "interaction-0001")
	if err != nil {
		t.Fatal(err)
	}
	if stub.target.GetCapability() != interactionapi.Capability || stub.target.GetOperation() != "cancel" || stub.call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_RUNNER || stub.call.GetTenantId() != "tenant-a" || stub.call.GetScene() != "runner.interaction" {
		t.Fatalf("Runner 调用上下文错误: target=%+v context=%+v", stub.target, stub.call)
	}
}
