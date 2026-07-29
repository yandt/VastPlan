// Package recoverycontroller projects a verified Seed Recovery Capsule from
// Node Agent actual state into local status, signed Node Leases and a bounded
// cluster aggregate. It is a kernel mechanism and does not execute recovery
// business actions or publish deployments.
package recoverycontroller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	recoveryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/recovery/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
)

const defaultReportMaxAge = 45 * time.Second

type LeasePublisher interface {
	UpdateRecovery(recoveryv1.NodeReport) error
}

type RecordVerifier func(controlplane.NodeRecord) error

type Controller struct {
	Capsule    recoveryv1.Capsule
	NodeID     string
	TenantID   string
	Deployment string
	Nodes      jetstream.KeyValue
	Verify     RecordVerifier
	StatusFile string
	Notify     func(recoveryv1.Status)
	Now        func() time.Time
	MaxAge     time.Duration

	mu     sync.RWMutex
	report recoveryv1.NodeReport
	lease  LeasePublisher
}

func New(capsule recoveryv1.Capsule, nodeID, tenant, deployment, statusFile string) (*Controller, error) {
	normalized, err := recoveryv1.NormalizeCapsule(capsule)
	if err != nil {
		return nil, err
	}
	if nodeID == "" || tenant == "" || deployment == "" || statusFile == "" {
		return nil, errors.New("Recovery Controller 必须配置 node、tenant、deployment 与状态文件")
	}
	return &Controller{
		Capsule: normalized, NodeID: nodeID, TenantID: tenant, Deployment: deployment,
		StatusFile: statusFile, Now: func() time.Time { return time.Now().UTC() }, MaxAge: defaultReportMaxAge,
	}, nil
}

func (c *Controller) AttachLease(lease LeasePublisher) error {
	if c == nil || lease == nil {
		return errors.New("Recovery Controller 租约发布器未配置")
	}
	c.mu.Lock()
	c.lease = lease
	report := recoveryv1.CloneNodeReport(c.report)
	c.mu.Unlock()
	if report.SchemaVersion == recoveryv1.Version {
		return lease.UpdateRecovery(report)
	}
	return nil
}

// Observe is suitable for Reconciler.StateObserver. It never reads desired
// configuration and cannot cause publication or plugin lifecycle changes.
func (c *Controller) Observe(actual nodeagent.ActualState) error {
	if c == nil {
		return errors.New("Recovery Controller 未配置")
	}
	now := c.now()
	units := make(map[string]recoveryv1.UnitObservation, len(actual.Units))
	for id, unit := range actual.Units {
		units[id] = recoveryv1.UnitObservation{Phase: string(unit.Phase), Readiness: unit.Readiness, Candidate: unit.Candidate != nil}
	}
	report := recoveryv1.BuildNodeReport(c.Capsule, recoveryv1.RuntimeObservation{
		NodeID: c.NodeID, ObservedRevision: actual.ObservedRevision, AppliedRevision: actual.AppliedRevision,
		UpdatedAt: actual.UpdatedAt, Units: units, ReconcileFailed: len(actual.Errors) > 0,
	}, recoveryv1.EvaluationPolicy{Now: now, MaxAge: c.maxAge()})
	status := recoveryv1.Aggregate(c.Capsule, []recoveryv1.NodeReport{report}, now, c.maxAge())
	if err := writeStatusFile(c.StatusFile, status); err != nil {
		return err
	}
	c.mu.Lock()
	c.report = recoveryv1.CloneNodeReport(report)
	lease, notify := c.lease, c.Notify
	c.mu.Unlock()
	if lease != nil {
		if err := lease.UpdateRecovery(report); err != nil {
			return fmt.Errorf("更新签名节点 Recovery 租约: %w", err)
		}
	}
	if notify != nil {
		notify(status)
	}
	return nil
}

func (c *Controller) Status(ctx context.Context) (recoveryv1.Status, error) {
	if c == nil {
		return recoveryv1.Status{}, errors.New("Recovery Controller 未配置")
	}
	reports, err := c.clusterReports(ctx)
	clusterAvailable := err == nil && c.Nodes != nil && c.Verify != nil
	if len(reports) == 0 {
		c.mu.RLock()
		if c.report.SchemaVersion == recoveryv1.Version {
			reports = append(reports, recoveryv1.CloneNodeReport(c.report))
		}
		c.mu.RUnlock()
	}
	status := recoveryv1.Aggregate(c.Capsule, reports, c.now(), c.maxAge())
	status.ClusterAvailable = clusterAvailable
	if clusterAvailable {
		status.Scope = "cluster"
	}
	return status, nil
}

func (c *Controller) now() time.Time {
	if c.Now == nil {
		return time.Now().UTC()
	}
	return c.Now().UTC()
}

func (c *Controller) maxAge() time.Duration {
	if c.MaxAge <= 0 {
		return defaultReportMaxAge
	}
	return c.MaxAge
}
