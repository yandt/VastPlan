package main

import (
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
)

func TestProjectRecoveryObservationNarrowsNodeAgentState(t *testing.T) {
	now := time.Now().UTC()
	observation := projectRecoveryObservation(nodeagent.ActualState{
		NodeID: "node-a", ObservedRevision: 7, AppliedRevision: 6, UpdatedAt: now,
		Errors: []nodeagent.OperationError{{UnitID: "database", Stage: "launch", Message: "sensitive process detail"}},
		Units: map[string]nodeagent.UnitState{
			"database": {
				Phase: nodeagent.PhaseActive, Readiness: "degraded", PIDs: []int{1234}, LastError: "sensitive error",
				Candidate: &nodeagent.CandidateState{Phase: nodeagent.PhaseFailed, LastError: "candidate detail"},
			},
		},
	})
	unit := observation.Units["database"]
	if observation.NodeID != "node-a" || observation.ObservedRevision != 7 || observation.AppliedRevision != 6 || !observation.UpdatedAt.Equal(now) || !observation.ReconcileFailed {
		t.Fatalf("Recovery 顶层观察投影错误: %+v", observation)
	}
	if unit.Phase != "active" || unit.Readiness != "degraded" || !unit.Candidate {
		t.Fatalf("Recovery unit 观察投影错误: %+v", unit)
	}
}
