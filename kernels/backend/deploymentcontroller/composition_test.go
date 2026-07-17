package deploymentcontroller

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/kernels/backend/nodeagent"
	deploymentv1 "cdsoft.com.cn/VastPlan/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/schemas/deployment/v2"
	"cdsoft.com.cn/VastPlan/shared/go/controlplane"
)

func TestObserveCompositionReportsDependencyLoss(t *testing.T) {
	_, buckets := startSchedulerNATS(t)
	actual := nodeagent.ActualState{Version: 2, NodeID: "node-a", Units: map[string]nodeagent.UnitState{
		"database": {Phase: nodeagent.PhaseActive, Readiness: "ready"},
		"api":      {Phase: nodeagent.PhaseActive, Readiness: "degraded", DependencyIssues: []string{"platform.database lease expired"}},
	}, UpdatedAt: time.Now().UTC()}
	raw, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buckets.Actual.Put(context.Background(), controlplane.ActualKey("node-a"), raw); err != nil {
		t.Fatal(err)
	}
	scheduler := Scheduler{Actual: buckets.Actual}
	report, err := scheduler.ObserveComposition(context.Background(), deploymentv2.Deployment{
		Metadata: deploymentv1.Metadata{Name: "prod"},
		Units:    []deploymentv2.ServiceUnit{{ID: "database", Enabled: true, Replicas: 1}, {ID: "api", Enabled: true, Replicas: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != CompositionDependencyLost {
		t.Fatalf("组合状态应为 DependencyLost: %+v", report)
	}
}
