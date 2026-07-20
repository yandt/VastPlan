package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
)

// natsDeploymentReadiness exposes the scheduler's durable composition report
// through a narrow trusted kernel service. Plugins never receive direct NATS
// access and cannot manufacture readiness observations.
type natsDeploymentReadiness struct {
	KV  jetstream.KeyValue
	now func() time.Time
}

func (o natsDeploymentReadiness) Observe(ctx context.Context, tenant, deployment string, expectedRevision uint64) (deploymentpublication.ReadinessObservation, error) {
	tenant, deployment = strings.TrimSpace(tenant), strings.TrimSpace(deployment)
	if o.KV == nil || tenant == "" || deployment == "" || expectedRevision == 0 {
		return deploymentpublication.ReadinessObservation{}, errors.New("部署 readiness 请求无效")
	}
	now := o.now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	entry, err := o.KV.Get(ctx, controlplane.CompositionKey(tenant, deployment))
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return pendingReadiness(tenant, deployment, expectedRevision, "report_missing", now()), nil
	}
	if err != nil {
		return deploymentpublication.ReadinessObservation{}, fmt.Errorf("读取组合 readiness: %w", err)
	}
	var observed deploymentpublication.ReadinessObservation
	decoder := json.NewDecoder(strings.NewReader(string(entry.Value())))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&observed); err != nil {
		return deploymentpublication.ReadinessObservation{}, fmt.Errorf("解析组合 readiness: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return deploymentpublication.ReadinessObservation{}, errors.New("组合 readiness 只能包含一个 JSON 文档")
	}
	if err := observed.Validate(); err != nil {
		return deploymentpublication.ReadinessObservation{}, err
	}
	if observed.Tenant != tenant || observed.Deployment != deployment {
		return deploymentpublication.ReadinessObservation{}, errors.New("组合 readiness 身份不匹配")
	}
	switch {
	case observed.Revision < expectedRevision:
		pending := pendingReadiness(tenant, deployment, expectedRevision, "revision_pending", now())
		pending.Generation = observed.Generation
		return pending, nil
	case observed.Revision > expectedRevision:
		return deploymentpublication.ReadinessObservation{
			SchemaVersion: 1, Tenant: tenant, Deployment: deployment,
			Revision: expectedRevision, Generation: observed.Generation,
			Status: deploymentpublication.ReadinessFailed, Reason: "revision_superseded", UpdatedAt: now(),
		}, nil
	default:
		return observed, nil
	}
}

func pendingReadiness(tenant, deployment string, revision uint64, reason string, at time.Time) deploymentpublication.ReadinessObservation {
	return deploymentpublication.ReadinessObservation{
		SchemaVersion: 1, Tenant: tenant, Deployment: deployment, Revision: revision,
		Status: deploymentpublication.ReadinessPending, Reason: reason, UpdatedAt: at.UTC(),
	}
}
