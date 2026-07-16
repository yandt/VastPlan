package addressing

import (
	"context"
	"errors"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/errorcode"
	"cdsoft.com.cn/VastPlan/shared/go/protocollimit"
)

func TestRouterLocalInvokeEnforcesPayloadAndPropagatesDeadline(t *testing.T) {
	router := &Router{
		Limits:      protocollimit.Limits{MaxPayloadBytes: 4, DefaultDeadline: 200 * time.Millisecond},
		local:       map[string][]localHandler{},
		localCursor: map[string]uint64{},
	}
	target := &contractv1.CallTarget{Capability: "demo.echo"}
	_, _, err := router.Invoke(context.Background(), target, nil, make([]byte, 5))
	var transportErr *TransportError
	if !errors.As(err, &transportErr) || transportErr.Code != errorcode.PayloadTooLarge {
		t.Fatalf("输入 payload 超限必须返回稳定错误码: %v", err)
	}

	router.local[target.Capability] = []localHandler{{handler: func(ctx context.Context, _ *contractv1.CallTarget, callCtx *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Second || callCtx == nil || callCtx.DeadlineUnixMs == nil {
			t.Fatalf("本地 fast path 也必须应用并传播默认 deadline: deadline=%v context=%+v", deadline, callCtx)
		}
		return okResult(), make([]byte, 5), nil
	}}}
	_, _, err = router.Invoke(context.Background(), target, nil, nil)
	if !errors.As(err, &transportErr) || transportErr.Code != errorcode.PayloadTooLarge {
		t.Fatalf("输出 payload 超限必须返回稳定错误码: %v", err)
	}
}

func TestStreamFrameAndInitialPayloadLimits(t *testing.T) {
	router := &Router{Limits: protocollimit.Limits{MaxPayloadBytes: 4, MaxStreamFrameBytes: 2}}
	_, err := router.InvokeStream(context.Background(), &contractv1.CallTarget{Capability: "demo.stream"}, nil, make([]byte, 5))
	var transportErr *TransportError
	if !errors.As(err, &transportErr) || transportErr.Code != errorcode.PayloadTooLarge {
		t.Fatalf("流式初始 payload 超限必须 fail-fast: %v", err)
	}
	remote := &RemoteStream{maxFrame: 2}
	if err := remote.Send(make([]byte, 3)); err == nil {
		t.Fatal("流式客户端必须在发送前拒绝超限帧")
	}
	server := &ServerStream{maxFrame: 2}
	if err := server.Send(make([]byte, 3)); err == nil {
		t.Fatal("流式服务端必须在发送前拒绝超限帧")
	}
}

func TestRouterConcurrencyGatesAreBounded(t *testing.T) {
	router := &Router{Limits: protocollimit.Limits{MaxConcurrentCalls: 1}}
	if !router.enterOutboundCall() {
		t.Fatal("第一个 outbound 调用应获准")
	}
	if router.enterOutboundCall() {
		t.Fatal("outbound 调用超过并发上限必须 fail-fast")
	}
	router.leaveOutboundCall()
	if !router.enterHandlerCall() {
		t.Fatal("第一个 handler 调用应获准")
	}
	if router.enterHandlerCall() {
		t.Fatal("handler 调用超过并发上限必须 fail-fast")
	}
	router.leaveHandlerCall()
}
