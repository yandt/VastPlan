// Package deploymentcontroller 把全局集群部署规格调度成每节点可执行快照。
//
// 控制器属于 Plugin Service 的期望态职责层；Node Agent 只消费 assignment 并执行，
// 不参与全局副本仲裁。
package deploymentcontroller

import (
	"context"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
)

type Scheduler struct {
	Nodes        jetstream.KeyValue
	Assignments  jetstream.KeyValue
	Metrics      jetstream.KeyValue
	Actual       jetstream.KeyValue
	Compositions jetstream.KeyValue
	Artifacts    ArtifactReader
}

type Plan struct {
	Generation  uint64
	Assignments map[string]deploymentv1.DesiredState
}

type scheduleState struct {
	SchemaVersion      int       `json:"schema_version"`
	Generation         uint64    `json:"generation"`
	DeploymentRevision uint64    `json:"deployment_revision"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// Reconcile 使用 rendezvous hashing 选节点，同一 unit 每节点至多一个副本。
// 容量不足时在写 assignment 前失败，避免把半份计划推给 Node Agent。
func (s Scheduler) Reconcile(ctx context.Context, deployment deploymentv2.Deployment) (Plan, error) {
	builder, err := newScheduleBuilder(ctx, s, deployment)
	if err != nil {
		return Plan{}, err
	}
	if err := builder.placeUnits(ctx); err != nil {
		return Plan{}, err
	}
	builder.localizeDependencies()
	return builder.publish(ctx)
}
