package main

import (
	recoveryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/recovery/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
)

func (r *reconcileRecovery) observe(actual nodeagent.ActualState) error {
	return r.controller.Observe(projectRecoveryObservation(actual))
}

// projectRecoveryObservation is the composition-root adapter between Node
// Agent implementation state and the bounded Recovery Controller contract.
func projectRecoveryObservation(actual nodeagent.ActualState) recoveryv1.RuntimeObservation {
	units := make(map[string]recoveryv1.UnitObservation, len(actual.Units))
	for id, unit := range actual.Units {
		units[id] = recoveryv1.UnitObservation{
			Phase: string(unit.Phase), Readiness: unit.Readiness, Candidate: unit.Candidate != nil,
		}
	}
	return recoveryv1.RuntimeObservation{
		NodeID: actual.NodeID, ObservedRevision: actual.ObservedRevision, AppliedRevision: actual.AppliedRevision,
		UpdatedAt: actual.UpdatedAt, Units: units, ReconcileFailed: len(actual.Errors) > 0,
	}
}
