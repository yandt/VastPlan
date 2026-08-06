package platformcontrol

import (
	"context"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	"cdsoft.com.cn/VastPlan/core/internal/callcontext"
	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	platformcontrolport "cdsoft.com.cn/VastPlan/extensions/libraries/go/platformcontrol"
)

type captureRouter struct {
	target *contractv1.CallTarget
	call   *contractv1.CallContext
	local  string
	items  []addressing.Announcement
}

func (r *captureRouter) InstancesFor(_, _, _ string) []addressing.Announcement {
	return append([]addressing.Announcement(nil), r.items...)
}

func (r *captureRouter) LocalNodeID() string { return r.local }

func (r *captureRouter) Invoke(_ context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
	r.target, r.call = target, call
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{}`), nil
}

func TestAddressingInvokerFixesTrustedIdentityAndRoute(t *testing.T) {
	router := &captureRouter{local: "node-local", items: []addressing.Announcement{
		{InstanceID: "remote-b", NodeID: "node-remote"},
		{InstanceID: "local-a", NodeID: "node-local"},
	}}
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
	instances := invoker.Instances(platformcontrolv1.BootstrapCapability)
	if len(instances) != 2 || instances[0] != (platformcontrolport.RuntimeInstance{ID: "local-a", NodeID: "node-local"}) {
		t.Fatalf("Runtime 实例未按本地优先排序: %+v", instances)
	}
	if _, _, err := invoker.InvokeInstance(context.Background(), platformcontrolv1.BootstrapCapability, operation, "remote-b", nil); err != nil || router.target.GetInstanceId() != "remote-b" {
		t.Fatalf("精确实例调用未固定 instance_id: target=%+v err=%v", router.target, err)
	}
}

func TestAddressingInvokerPreservesParentTraceForRuntimeDiagnostics(t *testing.T) {
	router := &captureRouter{}
	invoker, _ := NewAddressingInvoker(router)
	parent := callcontext.MustAdopt(&contractv1.CallContext{
		Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "cn.vastplan.platform.data.relational.connection-manager"},
		Trace:  &contractv1.Trace{TraceId: "0123456789abcdef0123456789abcdef", SpanId: "0123456789abcdef"},
	}, callcontext.Provenance{Source: "test", AuthenticatedBy: "test"})
	ctx := callcontext.WithTrusted(context.Background(), parent)
	if _, _, err := invoker.Invoke(ctx, platformcontrolv1.BootstrapCapability, platformcontrolv1.OperationTest, nil); err != nil {
		t.Fatal(err)
	}
	if router.call.GetTrace().GetTraceId() != parent.Wire().GetTrace().GetTraceId() {
		t.Fatalf("Platform Control 子调用丢失父 trace: %+v", router.call.GetTrace())
	}
}
