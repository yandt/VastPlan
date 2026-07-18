package plugin

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/errorcode"
	pluginhostv1 "cdsoft.com.cn/VastPlan/core/shared/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocollimit"
)

type lifecycleTestStream struct {
	grpc.ClientStream
	sent chan *pluginhostv1.FromPlugin
}

func (s *lifecycleTestStream) Send(msg *pluginhostv1.FromPlugin) error {
	s.sent <- msg
	return nil
}

func (s *lifecycleTestStream) Recv() (*pluginhostv1.FromHost, error) { return nil, context.Canceled }

func TestPluginRejectsInvokeBeforeSpawningPastConcurrencyLimit(t *testing.T) {
	p := New("test.plugin", "1.0.0", nil)
	p.Limits.MaxConcurrentCalls = 1
	p.active = true
	req := &pluginhostv1.InvokeRequest{Target: &contractv1.CallTarget{Capability: "test"}}
	if rejected := p.beginInvoke(req); rejected != nil {
		t.Fatalf("第一个调用应获准: %+v", rejected)
	}
	defer p.endInvoke()
	rejected := p.beginInvoke(req)
	if rejected == nil || rejected.Result.GetError().GetCode() != errorcode.ConcurrencyLimited {
		t.Fatalf("第二个调用必须在创建 goroutine 前被拒绝: %+v", rejected)
	}
}

func TestPluginDispatchEnforcesPayloadLimit(t *testing.T) {
	p := New("test.plugin", "1.0.0", nil)
	p.Limits.MaxPayloadBytes = 4
	resp := p.dispatchInvoke(&pluginhostv1.InvokeRequest{
		Target:  &contractv1.CallTarget{ExtensionPoint: "test.point", Capability: "test"},
		Payload: make([]byte, 5),
	})
	if resp.Result.GetError().GetCode() != errorcode.PayloadTooLarge {
		t.Fatalf("超限 payload 必须返回稳定错误码: %+v", resp)
	}
}

func TestPluginDispatchAppliesDefaultDeadline(t *testing.T) {
	p := New("test.plugin", "1.0.0", nil)
	p.Limits = protocollimit.Limits{DefaultDeadline: 200 * time.Millisecond}
	p.Contribute(Contribution{
		ExtensionPoint: "test.point", ID: "test",
		Handlers: map[string]Handler{"": func(ctx context.Context, _ Host, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > time.Second {
				t.Fatalf("SDK 必须给无 deadline 调用补默认上限: %v %v", deadline, ok)
			}
			return OK(0), nil, nil
		}},
	})
	resp := p.dispatchInvoke(&pluginhostv1.InvokeRequest{
		Target: &contractv1.CallTarget{ExtensionPoint: "test.point", Capability: "test"},
	})
	if resp.Result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("处理器应成功: %+v", resp)
	}
}

func TestPluginCancellationCannotRaceAheadOfHandlerStart(t *testing.T) {
	p := New("test.plugin", "1.0.0", nil)
	p.active = true
	p.Contribute(Contribution{
		ExtensionPoint: "test.point", ID: "test",
		Handlers: map[string]Handler{"": func(ctx context.Context, _ Host, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
			if ctx.Err() == nil {
				t.Fatal("在处理器启动前到达的 Cancel 必须被保留并传播")
			}
			return OK(0), nil, nil
		}},
	})
	req := &pluginhostv1.InvokeRequest{RequestId: "cancel-before-start",
		Target: &contractv1.CallTarget{ExtensionPoint: "test.point", Capability: "test"}}
	if rejected := p.beginInvoke(req); rejected != nil {
		t.Fatal(rejected)
	}
	p.cancelInvoke(req.RequestId)
	response := p.dispatchInvoke(req)
	p.endInvoke(req.RequestId)
	if response.Result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("协作取消测试处理器返回异常: %+v", response)
	}
}

func TestPluginCallContextInheritsInvocationPath(t *testing.T) {
	ctx := context.WithValue(context.Background(), invocationCallPathKey{}, []string{"tool.package/first"})
	original := &contractv1.CallContext{CallPath: []string{"forged/path"}}
	_, propagated, cancel := pluginCallContext(ctx, original, time.Second)
	defer cancel()
	if len(propagated.CallPath) != 1 || propagated.CallPath[0] != "tool.package/first" {
		t.Fatalf("SDK 必须继承处理器所属调用链，实际 %+v", propagated.CallPath)
	}
	if original.CallPath[0] != "forged/path" {
		t.Fatal("不得修改处理器持有的 CallContext")
	}
}

func TestDrainDoesNotBlockReadLoopWhileInflightCallWaits(t *testing.T) {
	p := New("test.plugin", "1.0.0", nil)
	p.active = true
	p.stream = &lifecycleTestStream{sent: make(chan *pluginhostv1.FromPlugin, 1)}
	req := &pluginhostv1.InvokeRequest{Target: &contractv1.CallTarget{Capability: "test"}}
	if rejected := p.beginInvoke(req); rejected != nil {
		t.Fatal("测试调用应获准")
	}

	queue := make(chan *pluginhostv1.Lifecycle, 1)
	done := make(chan struct{})
	defer close(done)
	go p.lifecycleLoop(queue, done)
	queue <- &pluginhostv1.Lifecycle{RequestId: "drain-1", Op: pluginhostv1.Lifecycle_OP_DRAIN}
	select {
	case <-p.stream.(*lifecycleTestStream).sent:
		t.Fatal("在途调用完成前不得发送 DRAIN Ack")
	default:
	}
	p.endInvoke()
	select {
	case msg := <-p.stream.(*lifecycleTestStream).sent:
		if msg.GetLifecycleAck().GetRequestId() != "drain-1" {
			t.Fatalf("收到错误 Ack: %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("在途调用完成后 DRAIN Ack 应立即收敛")
	}
}
