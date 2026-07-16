package addressing

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
)

func TestRemoteBidirectionalStream(t *testing.T) {
	server, buckets := startAddressingNATS(t)
	caller := newTestRouter(t, server, buckets.Capabilities, "stream-caller")
	worker := newTestRouter(t, server, buckets.Capabilities, "stream-worker")
	if err := caller.ConfigureStreamClient(StreamClientConfig{Insecure: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.StartStreamServer(StreamServerConfig{Insecure: true}); err != nil {
		t.Fatal(err)
	}
	registration, err := worker.RegisterStream(context.Background(), RegisterOptions{
		Capability: "demo.stream", ExtensionPoint: "tool.package", UnitID: "stream-worker",
	}, func(_ context.Context, target *contractv1.CallTarget, callCtx *contractv1.CallContext, initial []byte, stream *ServerStream) (*contractv1.CallResult, []byte, error) {
		if target.Capability != "demo.stream" || callCtx.TenantId != "tenant-a" || string(initial) != "hello" {
			t.Errorf("起始上下文未完整透传: target=%+v context=%+v initial=%q", target, callCtx, initial)
		}
		count := 0
		for {
			payload, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, nil, err
			}
			count++
			if err := stream.Send([]byte(strings.ToUpper(string(payload)))); err != nil {
				return nil, nil, err
			}
		}
		return okResult(), []byte{byte(count)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = registration.Close(context.Background()) }()
	waitInstances(t, caller, "demo.stream", 1)

	stream, err := caller.InvokeStream(context.Background(), target("demo.stream"), &contractv1.CallContext{TenantId: "tenant-a"}, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"first", "second"} {
		if err := stream.Send([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"FIRST", "SECOND"} {
		got, err := stream.Recv()
		if err != nil || string(got) != want {
			t.Fatalf("流式响应 got=%q want=%q err=%v", got, want, err)
		}
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("最终结果前应返回 EOF，实际 %v", err)
	}
	result, finalPayload, err := stream.Result()
	if err != nil || result.Status != contractv1.CallResult_STATUS_OK || len(finalPayload) != 1 || finalPayload[0] != 2 {
		t.Fatalf("流式最终结果错误 result=%+v payload=%v err=%v", result, finalPayload, err)
	}
}

func TestRemoteStreamCancellationPropagates(t *testing.T) {
	server, buckets := startAddressingNATS(t)
	caller := newTestRouter(t, server, buckets.Capabilities, "cancel-caller")
	worker := newTestRouter(t, server, buckets.Capabilities, "cancel-worker")
	_ = caller.ConfigureStreamClient(StreamClientConfig{Insecure: true})
	if _, err := worker.StartStreamServer(StreamServerConfig{Insecure: true}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	canceled := make(chan struct{})
	registration, err := worker.RegisterStream(context.Background(), RegisterOptions{
		Capability: "demo.cancel-stream", ExtensionPoint: "tool.package",
	}, func(ctx context.Context, _ *contractv1.CallTarget, _ *contractv1.CallContext, _ []byte, _ *ServerStream) (*contractv1.CallResult, []byte, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return nil, nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = registration.Close(context.Background()) }()
	waitInstances(t, caller, "demo.cancel-stream", 1)
	stream, err := caller.InvokeStream(context.Background(), target("demo.cancel-stream"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("远端流 handler 未启动")
	}
	stream.Cancel()
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("调用方取消未传播到远端 handler context")
	}
}

func TestStreamProductionSecurityFailsClosed(t *testing.T) {
	server, buckets := startAddressingNATS(t)
	router := newTestRouter(t, server, buckets.Capabilities, "secure-stream")
	if _, err := router.StartStreamServer(StreamServerConfig{}); err == nil {
		t.Fatal("生产流式服务缺少 mTLS 时必须 fail-closed")
	}
	if err := router.ConfigureStreamClient(StreamClientConfig{}); err == nil {
		t.Fatal("生产流式客户端缺少 TLS credentials 时必须 fail-closed")
	}
}
