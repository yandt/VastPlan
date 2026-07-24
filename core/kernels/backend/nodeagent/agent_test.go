package nodeagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
)

type deadlineSource struct{ deadline chan time.Duration }

type unpublishedSource struct {
	mu    sync.Mutex
	loads int
}

func (s *unpublishedSource) Load(context.Context) (deploymentv1.DesiredState, error) {
	s.mu.Lock()
	s.loads++
	s.mu.Unlock()
	return deploymentv1.DesiredState{}, ErrDesiredStateNotPublished
}

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

func TestAgentWaitsQuietlyBeforeFirstDesiredStatePublication(t *testing.T) {
	source := &unpublishedSource{}
	var mu sync.Mutex
	var logs []string
	agent := &Agent{
		Source: source, Reconciler: &Reconciler{Runtime: newFakeRuntime()}, Interval: 10 * time.Millisecond,
		Logf: func(format string, values ...any) {
			mu.Lock()
			logs = append(logs, fmt.Sprintf(format, values...))
			mu.Unlock()
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Millisecond)
	defer cancel()
	_ = agent.Run(ctx)
	source.mu.Lock()
	loads := source.loads
	source.mu.Unlock()
	mu.Lock()
	captured := append([]string(nil), logs...)
	mu.Unlock()
	if loads < 2 {
		t.Fatalf("等待态仍应轮询最新期望态: loads=%d", loads)
	}
	waiting, failures := 0, 0
	for _, line := range captured {
		if strings.Contains(line, "期望态尚未发布") {
			waiting++
		}
		if strings.Contains(line, "reconcile 未收敛") {
			failures++
		}
	}
	if waiting != 1 || failures != 0 {
		t.Fatalf("首次发布前只能记录一次等待状态: waiting=%d failures=%d logs=%v", waiting, failures, captured)
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
