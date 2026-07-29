package recoveryv1

import (
	"strings"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestAggregateRequiresPerUnitQuorumAcrossFreshNodes(t *testing.T) {
	now := time.Now().UTC()
	capsule := statusTestCapsule(2)
	reports := []NodeReport{
		BuildNodeReport(capsule, statusObservation("node-a", now, UnitReady), EvaluationPolicy{Now: now, MaxAge: time.Minute}),
		BuildNodeReport(capsule, statusObservation("node-b", now, UnitReady), EvaluationPolicy{Now: now, MaxAge: time.Minute}),
	}
	status := Aggregate(capsule, reports, now, 45*time.Second)
	if status.Overall != OverallPlatformReady || status.Nodes != 2 || status.Stages[0].Ready != 2 || status.Stages[0].Required != 2 {
		t.Fatalf("two fresh signed-equivalent reports should satisfy quorum: %+v", status)
	}

	reports[1].UpdatedAt = now.Add(-time.Minute)
	status = Aggregate(capsule, reports, now, 45*time.Second)
	if status.Overall != OverallStarting || status.Nodes != 1 || status.Stages[0].Status != UnitDegraded {
		t.Fatalf("stale report must not satisfy quorum: %+v", status)
	}
}

func TestAggregateRejectsDifferentCapsuleGeneration(t *testing.T) {
	now := time.Now().UTC()
	capsule := statusTestCapsule(1)
	report := BuildNodeReport(capsule, statusObservation("node-a", now, UnitReady), EvaluationPolicy{Now: now})
	report.Generation++
	status := Aggregate(capsule, []NodeReport{report}, now, time.Minute)
	if status.Nodes != 0 || status.Overall != OverallStarting {
		t.Fatalf("different Capsule generation must be ignored: %+v", status)
	}
}

func TestAggregatePreservesRecoveryReadyWhenLaterStageFails(t *testing.T) {
	now := time.Now().UTC()
	capsule := statusTestCapsule(1)
	report := BuildNodeReport(capsule, RuntimeObservation{
		NodeID: "node-a", ObservedRevision: 3, AppliedRevision: 3, UpdatedAt: now,
		Units: map[string]UnitObservation{
			"repository": {Phase: "active", Readiness: "ready"},
			"deployment": {Phase: "failed"},
			"database":   {Phase: "active", Readiness: "ready"},
		},
	}, EvaluationPolicy{Now: now})
	status := Aggregate(capsule, []NodeReport{report}, now, time.Minute)
	if status.Overall != OverallRecoveryReady || status.Stages[0].Status != UnitReady || status.Stages[1].Status != UnitFailed {
		t.Fatalf("later failure must not erase Recovery Ready: %+v", status)
	}
}

func statusTestCapsule(minReady uint16) Capsule {
	return Capsule{
		Version: Version, ID: "platform", Inventory: InventoryBinding{RepositoryID: "seed", Generation: 9},
		Artifacts: []Artifact{{Ref: pluginv1.ArtifactRef{PluginID: "repository", Version: "1.0.0", Channel: "stable"}, SHA256: strings.Repeat("a", 64)}},
		Stages: []Stage{
			{ID: StageRecovery, Units: []UnitRequirement{{ID: "repository", MinReady: minReady}}},
			{ID: StageControlPlane, Units: []UnitRequirement{{ID: "deployment", MinReady: minReady}}},
			{ID: StagePlatform, Units: []UnitRequirement{{ID: "database", MinReady: minReady}}},
		},
	}
}

func statusObservation(nodeID string, now time.Time, status string) RuntimeObservation {
	readiness, phase := "ready", "active"
	if status != UnitReady {
		readiness = "degraded"
	}
	return RuntimeObservation{
		NodeID: nodeID, ObservedRevision: 3, AppliedRevision: 3, UpdatedAt: now,
		Units: map[string]UnitObservation{
			"repository": {Phase: phase, Readiness: readiness},
			"deployment": {Phase: phase, Readiness: readiness},
			"database":   {Phase: phase, Readiness: readiness},
		},
	}
}
