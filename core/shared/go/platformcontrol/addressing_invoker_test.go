package platformcontrol

import (
	"context"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
)

type captureRouter struct {
	target *contractv1.CallTarget
	call   *contractv1.CallContext
}

func (r *captureRouter) Invoke(_ context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
	r.target, r.call = target, call
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{}`), nil
}

func TestAddressingInvokerFixesTrustedIdentityAndRoute(t *testing.T) {
	router := &captureRouter{}
	invoker, _ := NewAddressingInvoker(router)
	operation := platformcontrolv1.OperationOpen
	if _, _, err := invoker.Invoke(context.Background(), platformcontrolv1.BootstrapCapability, operation, nil); err != nil {
		t.Fatal(err)
	}
	if router.call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_SYSTEM || router.call.GetCaller().GetId() != platformcontrolv1.TrustedBootstrapSystemID {
		t.Fatalf("可信 caller 未固定: %+v", router.call)
	}
	if router.target.GetLogicalService() != platformcontrolv1.RuntimeLogicalService || router.target.GetRoutingDomain() != platformcontrolv1.RuntimeRoutingDomain {
		t.Fatalf("Runtime 路由未固定: %+v", router.target)
	}
	if _, _, err := invoker.Invoke(context.Background(), "business.database", operation, nil); err == nil {
		t.Fatal("Invoker 不得调用任意 capability")
	}
}
