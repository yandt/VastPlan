package nodeagent

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
)

const defaultReconcileTimeout = 15 * time.Minute

// FileSource 是本地开发期望态来源；集群模式替换为 NATS KV watch，Reconciler 不感知来源。
type FileSource struct {
	Path string
}

func (s FileSource) Load(_ context.Context) (deploymentv1.DesiredState, error) {
	return deploymentv1.ParseFile(s.Path)
}

// DesiredStateSource 隔离期望态存储，使本地文件与 NATS assignment 共用 Agent。
type DesiredStateSource interface {
	Load(context.Context) (deploymentv1.DesiredState, error)
}

// Agent 由启动、watch、运行时事件、轮询和退避重试共同触发对账；相同快照无副作用。
type Agent struct {
	Source     DesiredStateSource
	Reconciler *Reconciler
	Interval   time.Duration
	RetryMin   time.Duration
	RetryMax   time.Duration
	// ReconcileTimeout bounds one complete desired-to-actual convergence pass.
	// Zero selects 15 minutes, covering large artifact work without allowing a
	// permanently stuck driver to feed the service watchdog forever.
	ReconcileTimeout time.Duration
	Jitter           func(time.Duration) time.Duration
	Logf             func(string, ...any)
	// Pulse proves that the Agent control loop is still advancing. Production
	// wires it to systemd watchdog liveness; nil keeps tests and embeds simple.
	Pulse func()
	// BeginWork grants the service watchdog the same bounded window used by the
	// reconcile context. Nil disables this optional host integration.
	BeginWork func(time.Duration) func()
}

func (a *Agent) Run(ctx context.Context) error {
	if a.Reconciler == nil || a.Source == nil {
		return fmt.Errorf("agent source/reconciler 未配置")
	}
	interval := a.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if a.Logf == nil {
		a.Logf = func(string, ...any) {}
	}
	pulse := a.Pulse
	if pulse == nil {
		pulse = func() {}
	}
	pulse()
	var sourceEvents <-chan SourceEvent
	if watcher, ok := a.Source.(WatchableDesiredStateSource); ok {
		events, err := watcher.Watch(ctx)
		if err != nil {
			a.Logf("期望态 watch 启动失败，将保留轮询: %v", err)
		} else {
			sourceEvents = events
		}
	}
	retryMin, retryMax := a.RetryMin, a.RetryMax
	if retryMin <= 0 {
		retryMin = time.Second
	}
	if retryMax < retryMin {
		retryMax = 30 * time.Second
	}
	reconcileTimeout := a.ReconcileTimeout
	if reconcileTimeout <= 0 {
		reconcileTimeout = defaultReconcileTimeout
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var retry <-chan time.Time
	var retryTimer *time.Timer
	attempt := 0
	run := func(reason string) {
		pulse()
		finishWork := func() {}
		if a.BeginWork != nil {
			finishWork = a.BeginWork(reconcileTimeout)
		}
		reconcileCtx, cancelReconcile := context.WithTimeout(ctx, reconcileTimeout)
		err := a.reconcileOnce(reconcileCtx)
		cancelReconcile()
		finishWork()
		pulse()
		if err == nil {
			attempt = 0
			if retryTimer != nil && !retryTimer.Stop() {
				select {
				case <-retryTimer.C:
				default:
				}
			}
			retry = nil
			return
		}
		attempt++
		delay := backoff(retryMin, retryMax, attempt)
		if a.Jitter != nil {
			delay = a.Jitter(delay)
		} else {
			delay = defaultJitter(delay)
		}
		if delay < 0 {
			delay = 0
		}
		if retryTimer == nil {
			retryTimer = time.NewTimer(delay)
		} else {
			retryTimer.Reset(delay)
		}
		retry = retryTimer.C
		a.Logf("reconcile 未收敛 reason=%s attempt=%d retry=%v: %v", reason, attempt, delay, err)
	}
	run("startup")
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			pulse()
			if retry == nil {
				run("poll")
			}
		case event := <-a.Reconciler.Runtime.Events():
			pulse()
			a.Logf("运行时变化 unit=%s type=%s: %s", event.UnitID, event.Type, event.Message)
			if retry == nil {
				run("runtime_event")
			}
		case event, ok := <-sourceEvents:
			pulse()
			if !ok {
				sourceEvents = nil
				a.Logf("期望态 watch 已关闭，将保留轮询")
				continue
			}
			if event.Err != nil {
				a.Logf("期望态 watch 错误: %v", event.Err)
			}
			if retry == nil {
				run("source_watch")
			}
		case <-retry:
			pulse()
			retry = nil
			run("retry")
		}
	}
}

func (a *Agent) reconcileOnce(ctx context.Context) error {
	desired, err := a.Source.Load(ctx)
	if err != nil {
		return err
	}
	result, err := a.Reconciler.Reconcile(ctx, desired)
	if err == nil && result.Changed {
		a.Logf("已收敛 revision=%d units=%d", desired.Revision, len(result.State.Units))
	}
	return err
}

func backoff(minimum, maximum time.Duration, attempt int) time.Duration {
	delay := minimum
	for i := 1; i < attempt && delay < maximum; i++ {
		if delay > maximum/2 {
			return maximum
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}

func defaultJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return delay
	}
	// 80%–120% 抖动，防止控制面恢复时多个节点同时冲击仓库或 NATS。
	return time.Duration(float64(delay) * (0.8 + rand.Float64()*0.4))
}
