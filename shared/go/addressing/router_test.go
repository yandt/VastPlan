package addressing

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/controlplane"
)

func TestRouterLocalAndRemoteInvoke(t *testing.T) {
	server, buckets := startAddressingNATS(t)
	caller := newTestRouter(t, server, buckets.Capabilities, "caller")
	worker := newTestRouter(t, server, buckets.Capabilities, "worker")

	var receivedTarget *contractv1.CallTarget
	registration, err := worker.Register(context.Background(), RegisterOptions{
		Capability: "demo.echo", ExtensionPoint: "tool.package", ServiceRole: "backend", UnitID: "worker-unit",
	}, func(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		receivedTarget = target
		return okResult(), append([]byte("echo:"), payload...), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registration.Close(context.Background()) })

	target := &contractv1.CallTarget{ExtensionPoint: "tool.package", Capability: "demo.echo"}
	result, payload, err := worker.Invoke(context.Background(), target, nil, []byte("local"))
	if err != nil || result.Status != contractv1.CallResult_STATUS_OK || string(payload) != "echo:local" {
		t.Fatalf("本地调用失败: result=%+v payload=%q err=%v", result, payload, err)
	}
	if receivedTarget != target {
		t.Fatal("本地 capability 命中应函数直调，不应经过 protobuf 复制")
	}

	waitInstances(t, caller, "demo.echo", 1)
	receivedTarget = nil
	result, payload, err = caller.Invoke(context.Background(), target, &contractv1.CallContext{TenantId: "tenant-a"}, []byte("remote"))
	if err != nil || result.Status != contractv1.CallResult_STATUS_OK || string(payload) != "echo:remote" {
		t.Fatalf("远端调用失败: result=%+v payload=%q err=%v", result, payload, err)
	}
	if receivedTarget == target {
		t.Fatal("远端 capability 应经过寻址 wire，不能共享调用方指针")
	}
}

func TestRouterQueueGroupErrorsCancellationAndWithdraw(t *testing.T) {
	server, buckets := startAddressingNATS(t)
	caller := newTestRouter(t, server, buckets.Capabilities, "caller")
	workerA := newTestRouter(t, server, buckets.Capabilities, "worker-a")
	workerB := newTestRouter(t, server, buckets.Capabilities, "worker-b")

	var hitsA, hitsB atomic.Int64
	register := func(router *Router, unit string, hits *atomic.Int64) *Registration {
		registration, err := router.Register(context.Background(), RegisterOptions{
			Capability: "demo.balanced", ExtensionPoint: "tool.package", UnitID: unit,
		}, func(_ context.Context, _ *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			hits.Add(1)
			return okResult(), payload, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return registration
	}
	registrationA := register(workerA, "a", &hitsA)
	registrationB := register(workerB, "b", &hitsB)
	t.Cleanup(func() {
		_ = registrationA.Close(context.Background())
		_ = registrationB.Close(context.Background())
	})
	waitInstances(t, caller, "demo.balanced", 2)
	for range 30 {
		if _, _, err := caller.Invoke(context.Background(), target("demo.balanced"), nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if hitsA.Load() == 0 || hitsB.Load() == 0 || hitsA.Load()+hitsB.Load() != 30 {
		t.Fatalf("queue group 未把调用分发到两个实例: a=%d b=%d", hitsA.Load(), hitsB.Load())
	}

	application, err := workerA.Register(context.Background(), RegisterOptions{
		Capability: "demo.application-error", ExtensionPoint: "tool.package", UnitID: "application",
	}, func(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "demo.rejected"}}, nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close(context.Background()) })
	waitInstances(t, caller, "demo.application-error", 1)
	result, _, err := caller.Invoke(context.Background(), target("demo.application-error"), nil, nil)
	if err != nil || result.GetError().GetCode() != "demo.rejected" {
		t.Fatalf("应用错误不应提升成传输错误: result=%+v err=%v", result, err)
	}

	transport, err := workerA.Register(context.Background(), RegisterOptions{
		Capability: "demo.transport-error", ExtensionPoint: "tool.package", UnitID: "transport",
	}, func(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
		return nil, nil, errors.New("worker unavailable")
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = transport.Close(context.Background()) })
	waitInstances(t, caller, "demo.transport-error", 1)
	_, _, err = caller.Invoke(context.Background(), target("demo.transport-error"), nil, nil)
	var transportErr *TransportError
	if !errors.As(err, &transportErr) || transportErr.Code != "remote.invoke_failed" {
		t.Fatalf("handler 故障应返回独立 TransportError: %T %v", err, err)
	}

	started := make(chan struct{})
	canceled := make(chan struct{})
	cancelRegistration, err := workerA.Register(context.Background(), RegisterOptions{
		Capability: "demo.cancel", ExtensionPoint: "tool.package", UnitID: "cancel",
	}, func(ctx context.Context, _ *contractv1.CallTarget, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return nil, nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cancelRegistration.Close(context.Background()) })
	waitInstances(t, caller, "demo.cancel", 1)
	invokeCtx, cancel := context.WithCancel(context.Background())
	invokeDone := make(chan error, 1)
	go func() {
		_, _, invokeErr := caller.Invoke(invokeCtx, target("demo.cancel"), nil, nil)
		invokeDone <- invokeErr
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("远端 handler 未启动")
	}
	cancel()
	select {
	case <-invokeDone:
	case <-time.After(3 * time.Second):
		t.Fatal("调用方取消后 Invoke 未返回")
	}
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("调用方取消未传播到远端 handler")
	}

	if err := registrationA.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := registrationB.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitInstances(t, caller, "demo.balanced", 0)
	_, _, err = caller.Invoke(context.Background(), target("demo.balanced"), nil, nil)
	if !errors.Is(err, ErrCapabilityNotFound) {
		t.Fatalf("所有实例撤销后应在目录层失败: %v", err)
	}
}

func TestRouterEventFanout(t *testing.T) {
	server, buckets := startAddressingNATS(t)
	publisher := newTestRouter(t, server, buckets.Capabilities, "publisher")
	subscriber := newTestRouter(t, server, buckets.Capabilities, "subscriber")

	received := make(chan *contractv1.CallEvent, 1)
	subscription, err := subscriber.Subscribe("task.completed", func(_ context.Context, _ *contractv1.CallContext, event *contractv1.CallEvent) error {
		received <- event
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = subscription.Close() })
	event := &contractv1.CallEvent{Id: "event-1", Type: "task.completed", Source: "test", Payload: []byte("done")}
	if err := publisher.Publish(context.Background(), &contractv1.CallContext{TenantId: "tenant-a"}, event); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-received:
		if got.Id != event.Id || string(got.Payload) != "done" {
			t.Fatalf("事件内容不匹配: %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("未收到 Core NATS 事件")
	}
}

func TestRouterMultipleLocalRegistrationsRemainRoutable(t *testing.T) {
	server, buckets := startAddressingNATS(t)
	router := newTestRouter(t, server, buckets.Capabilities, "worker")
	var hitsA, hitsB atomic.Int64
	register := func(unit string, hits *atomic.Int64) *Registration {
		registration, err := router.Register(context.Background(), RegisterOptions{
			Capability: "demo.local-replicas", ExtensionPoint: "tool.package", UnitID: unit,
		}, func(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
			hits.Add(1)
			return okResult(), nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return registration
	}
	first := register("first", &hitsA)
	second := register("second", &hitsB)
	for range 10 {
		if _, _, err := router.Invoke(context.Background(), target("demo.local-replicas"), nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if hitsA.Load() != 5 || hitsB.Load() != 5 {
		t.Fatalf("本地多实例未轮询: first=%d second=%d", hitsA.Load(), hitsB.Load())
	}
	if err := second.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := router.Invoke(context.Background(), target("demo.local-replicas"), nil, nil); err != nil {
		t.Fatalf("关闭一个 registration 不应误删剩余本地实例: %v", err)
	}
	if hitsA.Load() != 6 {
		t.Fatalf("剩余本地实例未收到调用: %d", hitsA.Load())
	}
	if err := first.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func startAddressingNATS(t *testing.T) (*natsserver.Server, controlplane.Buckets) {
	t.Helper()
	server, err := natsserver.NewServer(&natsserver.Options{
		JetStream: true, StoreDir: filepath.Join(t.TempDir(), "jetstream"),
		Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	go server.Start()
	if !server.ReadyForConnections(10 * time.Second) {
		t.Fatal("嵌入式 NATS 未就绪")
	}
	if _, ok := server.Addr().(*net.TCPAddr); !ok {
		t.Fatal("嵌入式 NATS 未监听 TCP")
	}
	t.Cleanup(func() {
		server.Shutdown()
		server.WaitForShutdown()
	})
	nc, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	buckets, err := controlplane.EnsureBuckets(context.Background(), js, 1, jetstream.MemoryStorage)
	if err != nil {
		t.Fatal(err)
	}
	return server, buckets
}

func newTestRouter(t *testing.T, server *natsserver.Server, directory jetstream.KeyValue, nodeID string) *Router {
	t.Helper()
	nc, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(nc, directory, nodeID, t.Logf)
	if err != nil {
		nc.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = router.Close()
		nc.Close()
	})
	return router
}

func waitInstances(t *testing.T, router *Router, capability string, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(router.Instances(capability)) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待 capability=%s instances=%d 超时，当前=%d", capability, want, len(router.Instances(capability)))
}

func target(capability string) *contractv1.CallTarget {
	return &contractv1.CallTarget{ExtensionPoint: "tool.package", Capability: capability}
}

func okResult() *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}
}
