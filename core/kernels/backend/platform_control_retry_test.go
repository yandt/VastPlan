package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
)

// countingController fails Start until it is told to settle, standing in for a
// Database Runtime whose capability is not yet routable.
type countingController struct {
	mu      sync.Mutex
	starts  int
	settled bool
}

func (c *countingController) Start(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.starts++
	if c.settled {
		return nil
	}
	return errors.New("shared state provider unavailable")
}

func (c *countingController) settle() {
	c.mu.Lock()
	c.settled = true
	c.mu.Unlock()
}

func (c *countingController) startCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.starts
}

func (c *countingController) Status() platformcontrolv1.Status { return platformcontrolv1.Status{} }

func (c *countingController) TestCandidate(context.Context, platformcontrolv1.ChangeRequest) error {
	return nil
}

func (c *countingController) Configure(context.Context, platformcontrolv1.ChangeRequest) error {
	return nil
}

// closingTopology hands out a subscription that is already closed, which is what
// a rebuilt or closed Router does.
type closingTopology struct {
	mu          sync.Mutex
	subscribes  int
	closeUpTo   int
	liveChannel chan struct{}
}

func (t *closingTopology) SubscribeTopologyChanges() (<-chan struct{}, func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.subscribes++
	if t.subscribes <= t.closeUpTo {
		closed := make(chan struct{})
		close(closed)
		return closed, func() {}
	}
	t.liveChannel = make(chan struct{}, 1)
	return t.liveChannel, func() {}
}

func (t *closingTopology) subscribeCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.subscribes
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("超时等待: %s", what)
}

// A closed subscription used to end Run permanently. Since reconcile has no
// other caller, Open would then never be retried and the platform stayed
// unbound until the kernel was restarted.
func TestRunResubscribesAfterSubscriptionCloses(t *testing.T) {
	controller := &countingController{}
	topology := &closingTopology{closeUpTo: 2}
	coordinator := &platformControlCoordinator{
		controller: controller,
		topology:   topology,
		logf:       func(string, ...any) {},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		coordinator.Run(ctx)
		close(done)
	}()

	waitFor(t, "订阅关闭后必须重新订阅", func() bool { return topology.subscribeCount() > 2 })
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run 未能随 context 退出")
	}
}

// A reconcile that failed while no further topology edge is coming must still
// be retried, otherwise the platform is stranded with a committed profile.
func TestRunRetriesFailedReconcileWithoutTopologyEdge(t *testing.T) {
	controller := &countingController{}
	topology := &closingTopology{}
	coordinator := &platformControlCoordinator{
		controller: controller,
		topology:   topology,
		logf:       func(string, ...any) {},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go coordinator.Run(ctx)

	waitFor(t, "失败的 reconcile 必须自行重试", func() bool { return controller.startCount() >= 3 })

	controller.settle()
	waitFor(t, "settle 后必须停止重试", func() bool {
		first := controller.startCount()
		time.Sleep(300 * time.Millisecond)
		return controller.startCount() == first
	})
}
