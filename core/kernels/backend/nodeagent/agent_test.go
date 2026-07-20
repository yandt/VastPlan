package nodeagent

import (
	"context"
	"errors"
	"testing"
	"time"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
)

type deadlineSource struct{ deadline chan time.Duration }

func (s deadlineSource) Load(ctx context.Context) (deploymentv1.DesiredState, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return deploymentv1.DesiredState{}, errors.New("reconcile context 缺少 deadline")
	}
	s.deadline <- time.Until(deadline)
	<-ctx.Done()
	return deploymentv1.DesiredState{}, ctx.Err()
}

func TestBackoffIsExponentialAndCapped(t *testing.T) {
	minimum, maximum := 100*time.Millisecond, 750*time.Millisecond
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 750 * time.Millisecond, 750 * time.Millisecond}
	for i, expected := range want {
		if got := backoff(minimum, maximum, i+1); got != expected {
			t.Fatalf("attempt %d backoff=%v want=%v", i+1, got, expected)
		}
	}
}

func TestAgentBoundsReconcileAndWatchdogWorkLeaseTogether(t *testing.T) {
	const reconcileTimeout = 50 * time.Millisecond
	deadlines := make(chan time.Duration, 1)
	leases := make(chan time.Duration, 1)
	agent := &Agent{
		Source: deadlineSource{deadline: deadlines}, Reconciler: &Reconciler{Runtime: newFakeRuntime()},
		ReconcileTimeout: reconcileTimeout, RetryMin: time.Hour, RetryMax: time.Hour,
		Jitter: func(delay time.Duration) time.Duration { return delay },
		BeginWork: func(duration time.Duration) func() {
			leases <- duration
			return func() {}
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx) }()

	if lease := <-leases; lease != reconcileTimeout {
		t.Fatalf("watchdog 工作租约=%v want=%v", lease, reconcileTimeout)
	}
	if deadline := <-deadlines; deadline <= 0 || deadline > reconcileTimeout {
		t.Fatalf("reconcile deadline 未受相同上限约束: %v", deadline)
	}
	time.Sleep(2 * reconcileTimeout)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Agent 退出原因=%v want=context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Agent 未在取消后退出")
	}
}
