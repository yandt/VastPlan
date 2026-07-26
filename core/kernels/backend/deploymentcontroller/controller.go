package deploymentcontroller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"github.com/nats-io/nats.go/jetstream"
)

// Controller watch 全局部署和节点租约，任一变化都重新生成 assignment；周期轮询负责恢复 watcher 漏报。
type Controller struct {
	Deployments   jetstream.KeyValue
	Scheduler     Scheduler
	Leaders       jetstream.KeyValue
	DeploymentKey string
	Identity      string
	Interval      time.Duration
	Election      controlplane.LeaderElectionOptions
	Logf          func(string, ...any)
}

// convergenceSummary 是一次成功对账中值得写入 INFO 的稳定业务摘要。
// 节点续租和实际态 checkpoint 都会产生 KV 事件；相同摘要不应被误解为
// 一次新的调度状态变化。
type convergenceSummary struct {
	generation        uint64
	nodes             int
	compositionStatus string
	units             int
	hasComposition    bool
}

func shouldLogConvergence(last *convergenceSummary, current convergenceSummary) bool {
	return last == nil || *last != current
}

func watchEntryMatches(entry jetstream.KeyValueEntry, prefix string) bool {
	return entry != nil && watchKeyMatches(entry.Key(), prefix)
}

func watchKeyMatches(key, prefix string) bool {
	return key != "" && strings.HasPrefix(key, prefix)
}

func (c Controller) Run(ctx context.Context) error {
	if c.Deployments == nil || c.DeploymentKey == "" || c.Leaders == nil || c.Identity == "" {
		return errors.New("controller deployment/leader KV、deployment key 与 identity 未配置")
	}
	if c.Scheduler.Nodes == nil || c.Scheduler.Assignments == nil {
		return errors.New("controller scheduler KV 未配置")
	}
	if c.Scheduler.ContractCache == nil {
		c.Scheduler.ContractCache = &ContractValidationCache{}
	}
	if c.Interval <= 0 {
		c.Interval = 5 * time.Second
	}
	if c.Logf == nil {
		c.Logf = func(string, ...any) {}
	}
	c.Election.Logf = c.Logf
	elector := controlplane.LeaderElector{
		KV: c.Leaders, Election: c.DeploymentKey, Identity: c.Identity,
		Options: c.Election,
	}
	for {
		leadership, err := elector.Acquire(ctx)
		if err != nil {
			return err
		}
		record := leadership.Record()
		c.Logf("controller 获得领导权 identity=%s election=%s token=%s", c.Identity, c.DeploymentKey, record.Token)
		leaderCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- c.runAsLeader(leaderCtx) }()
		select {
		case <-ctx.Done():
			cancel()
			<-done
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = leadership.Close(closeCtx)
			closeCancel()
			return ctx.Err()
		case lost := <-leadership.Lost():
			cancel()
			<-done
			c.Logf("controller 失去领导权 identity=%s: %v", c.Identity, lost)
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = leadership.Close(closeCtx)
			closeCancel()
			// 领导权丢失不是进程故障；回到 Acquire 等待当前 leader 退出或租约过期。
			continue
		case runErr := <-done:
			cancel()
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = leadership.Close(closeCtx)
			closeCancel()
			return runErr
		}
	}
}

func (c Controller) runAsLeader(ctx context.Context) error {
	deploymentWatcher, err := c.Deployments.Watch(ctx, c.DeploymentKey)
	if err != nil {
		return fmt.Errorf("watch 集群部署: %w", err)
	}
	defer func() {
		_ = deploymentWatcher.Stop() // context 结束后只做 watcher 本地回收，主错误更有诊断价值。
	}()
	nodeWatcher, err := c.Scheduler.Nodes.WatchAll(ctx)
	if err != nil {
		return fmt.Errorf("watch 节点租约: %w", err)
	}
	defer func() {
		_ = nodeWatcher.Stop() // 同上；停止失败不覆盖 controller 的退出原因。
	}()
	var actualWatcher jetstream.KeyWatcher
	if c.Scheduler.Actual != nil {
		actualWatcher, err = c.Scheduler.Actual.WatchAll(ctx)
		if err != nil {
			return fmt.Errorf("watch 节点实际态: %w", err)
		}
		defer func() { _ = actualWatcher.Stop() }()
	}
	var actualUpdates <-chan jetstream.KeyValueEntry
	if actualWatcher != nil {
		actualUpdates = actualWatcher.Updates()
	}
	var lastSummary *convergenceSummary
	deploymentMissing := false
	reconcile := func(reason string) {
		entry, err := c.Deployments.Get(ctx, c.DeploymentKey)
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			if !deploymentMissing {
				c.Logf("集群部署尚未发布，等待 KV 创建 key=%s", c.DeploymentKey)
				deploymentMissing = true
			}
			return
		}
		if err != nil {
			c.Logf("读取集群部署失败 reason=%s: %v", reason, err)
			return
		}
		if deploymentMissing {
			c.Logf("集群部署已发布 key=%s", c.DeploymentKey)
			deploymentMissing = false
		}
		deployment, err := deploymentv2.Parse(entry.Value())
		if err == nil {
			var plan Plan
			plan, err = c.Scheduler.Reconcile(ctx, deployment)
			if err == nil {
				summary := convergenceSummary{generation: plan.Generation, nodes: len(plan.Assignments)}
				if c.Scheduler.Actual != nil {
					if report, observeErr := c.Scheduler.ObserveComposition(ctx, deployment); observeErr == nil {
						summary.compositionStatus = string(report.Status)
						summary.units = len(report.Units)
						summary.hasComposition = true
					} else {
						c.Logf("组合状态观测失败 reason=%s: %v", reason, observeErr)
					}
				}
				if shouldLogConvergence(lastSummary, summary) {
					c.Logf("调度已收敛 reason=%s generation=%d nodes=%d", reason, plan.Generation, len(plan.Assignments))
					if summary.hasComposition {
						c.Logf("组合状态 reason=%s status=%s units=%d", reason, summary.compositionStatus, summary.units)
					}
					lastSummary = &summary
				}
			}
		}
		if err != nil {
			c.Logf("调度未收敛 reason=%s: %v", reason, err)
		}
	}
	reconcile("startup")
	ticker := time.NewTicker(c.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			reconcile("poll")
		case _, ok := <-deploymentWatcher.Updates():
			if !ok {
				return errors.New("集群部署 watcher 已关闭")
			}
			reconcile("deployment_watch")
		case entry, ok := <-nodeWatcher.Updates():
			if !ok {
				return errors.New("节点 watcher 已关闭")
			}
			if !watchEntryMatches(entry, controlplane.AssignmentPrefixForDeploymentKey(c.DeploymentKey)) {
				continue
			}
			reconcile("node_watch")
		case entry, ok := <-actualUpdates:
			if !ok {
				return errors.New("节点实际态 watcher 已关闭")
			}
			if !watchEntryMatches(entry, controlplane.ActualPrefixForDeploymentKey(c.DeploymentKey)) {
				continue
			}
			reconcile("actual_watch")
		}
	}
}
