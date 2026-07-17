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
	desired := deploymentv1.DesiredState{Version: 1, Revision: 7, Metadata: deploymentv1.Metadata{Name: "prod", Tenant: "acme"}, Units: []deploymentv1.Unit{
		{ID: "database", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1, Plugins: []deploymentv1.PluginRef{{ID: "com.example.db", Version: "1.0.0"}}},
		{ID: "api", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1, Plugins: []deploymentv1.PluginRef{{ID: "com.example.api", Version: "1.0.0"}}},
	}}
	desiredRaw, _ := json.Marshal(desired)
	if _, _, err := controlplane.ApplyDesiredState(context.Background(), buckets.Assignments, controlplane.AssignmentKey("acme", "prod", "node-a"), desiredRaw); err != nil {
		t.Fatal(err)
	}
	scheduler := Scheduler{Actual: buckets.Actual, Assignments: buckets.Assignments, Compositions: buckets.Compositions}
	report, err := scheduler.ObserveComposition(context.Background(), deploymentv2.Deployment{
		Version: 2, Revision: 3, Metadata: deploymentv1.Metadata{Name: "prod", Tenant: "acme"},
		Units: []deploymentv2.ServiceUnit{{ID: "database", Enabled: true, Replicas: 1}, {ID: "api", Enabled: true, Replicas: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != CompositionDependencyLost {
		t.Fatalf("组合状态应为 DependencyLost: %+v", report)
	}
	entry, err := buckets.Compositions.Get(context.Background(), controlplane.CompositionKey("acme", "prod"))
	if err != nil || len(entry.Value()) == 0 || report.Generation != 7 {
		t.Fatalf("组合状态必须隔离持久化并携带 generation: report=%+v err=%v", report, err)
	}
}

func TestObserveCompositionReportsContractedPartitionOwnersAsDegraded(t *testing.T) {
	_, buckets := startSchedulerNATS(t)
	ctx := context.Background()
	actual := nodeagent.ActualState{Version: 2, NodeID: "node-a", Units: map[string]nodeagent.UnitState{
		"database": {Phase: nodeagent.PhaseActive, Readiness: "ready"},
	}, UpdatedAt: time.Now().UTC()}
	raw, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buckets.Actual.Put(ctx, controlplane.ActualKey("node-a"), raw); err != nil {
		t.Fatal(err)
	}
	assignment := deploymentv1.DesiredState{
		Version: 1, Revision: 8, Metadata: deploymentv1.Metadata{Name: "prod", Tenant: "acme"},
		Units: []deploymentv1.Unit{{
			ID: "database", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{{ID: "com.example.database", Version: "1.0.0"}},
		}},
	}
	assignmentRaw, _ := json.Marshal(assignment)
	if _, _, err := controlplane.ApplyDesiredState(ctx, buckets.Assignments, controlplane.AssignmentKey("acme", "prod", "node-a"), assignmentRaw); err != nil {
		t.Fatal(err)
	}
	report, err := (Scheduler{Actual: buckets.Actual, Assignments: buckets.Assignments}).ObserveComposition(ctx, deploymentv2.Deployment{
		Version: 2, Revision: 4, Metadata: deploymentv1.Metadata{Name: "prod", Tenant: "acme"},
		Units: []deploymentv2.ServiceUnit{{ID: "database", Enabled: true, Replicas: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != CompositionDegraded || len(report.Units) != 1 || report.Units[0].DesiredReplicas != 2 || report.Units[0].ReadyReplicas != 1 {
		t.Fatalf("owner 收缩必须按部署目标报告 Degraded: %+v", report)
	}
}
