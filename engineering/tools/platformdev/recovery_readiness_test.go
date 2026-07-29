package main

import (
	"testing"
	"time"

	recoveryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/recovery/v1"
)

func TestEvaluateRecoveryCapsuleReportsCumulativeStages(t *testing.T) {
	capsule := recoveryv1.Capsule{ID: "platform", Stages: []recoveryv1.Stage{
		{ID: recoveryv1.StageRecovery, Units: []string{"repository"}},
		{ID: recoveryv1.StageControlPlane, Units: []string{"deployment"}},
		{ID: recoveryv1.StagePlatform, Units: []string{"database"}},
	}}
	startedAt := time.Now().UTC()
	state := readinessState{
		ObservedRevision: 2, AppliedRevision: 2, UpdatedAt: startedAt.Add(time.Second),
		Units: map[string]readinessUnit{
			"repository": {Phase: "active", Readiness: "ready"},
			"deployment": {Phase: "active", Readiness: "ready"},
			"database":   {Phase: "starting", Readiness: "pending"},
		},
	}
	status := evaluateRecoveryCapsule(capsule, state, startedAt)
	if status.Overall != "ControlPlaneReady" || status.Stages[0].Status != "Ready" || status.Stages[1].Status != "Ready" || status.Stages[2].Status != "Pending" {
		t.Fatalf("unexpected recovery status: %+v", status)
	}
}

func TestEvaluateRecoveryCapsuleRejectsStaleActualState(t *testing.T) {
	startedAt := time.Now().UTC()
	capsule := recoveryv1.Capsule{ID: "platform", Stages: []recoveryv1.Stage{
		{ID: recoveryv1.StageRecovery, Units: []string{"repository"}},
		{ID: recoveryv1.StageControlPlane, Units: []string{"deployment"}},
		{ID: recoveryv1.StagePlatform, Units: []string{"database"}},
	}}
	status := evaluateRecoveryCapsule(capsule, readinessState{
		ObservedRevision: 2, AppliedRevision: 2, UpdatedAt: startedAt.Add(-time.Second),
		Units: map[string]readinessUnit{"repository": {Phase: "active", Readiness: "ready"}},
	}, startedAt)
	if status.Overall != "Starting" || status.Stages[0].Status != "Pending" {
		t.Fatalf("stale state must not satisfy recovery readiness: %+v", status)
	}
}
