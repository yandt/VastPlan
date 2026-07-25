package nodeagent

import (
	"context"
	"errors"
	"fmt"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/servicemodel"
)

func (r *Reconciler) reconcileTargets(ctx context.Context, revision uint64, targets map[string]deploymentv1.Unit, actual *ActualState) (bool, error) {
	changed := false
	graph := make(map[string][]string, len(targets))
	for id, unit := range targets {
		graph[id] = append([]string(nil), unit.DependsOn...)
	}
	ordered, err := servicemodel.TopologicalOrder(graph)
	if err != nil {
		return false, fmt.Errorf("节点 %s 本地 unit 依赖图无效: %w", r.NodeID, err)
	}
	for _, id := range ordered {
		r.pulse()
		unitChanged, err := r.reconcileTarget(ctx, revision, targets[id], actual)
		changed = changed || unitChanged
		if err != nil {
			return changed, err
		}
	}
	return changed, nil
}

func (r *Reconciler) reconcileTarget(ctx context.Context, revision uint64, unit deploymentv1.Unit, actual *ActualState) (bool, error) {
	r.pulse()
	id, fingerprint := unit.ID, unit.Fingerprint()
	policy, err := unitPolicy(unit)
	if err != nil {
		return false, err
	}
	if current, ok := actual.Units[id]; ok && current.Fingerprint == fingerprint && r.Runtime.IsRunning(id, fingerprint) {
		revisionChanged := current.AppliedRevision != revision
		if err := r.setUnitPhase(&current, PhaseActive); err != nil {
			return false, err
		}
		// A new assignment generation can preserve the exact same unit
		// fingerprint (for example, a surviving replica after node loss). The
		// runtime is already correct, but the controller must still be able to
		// prove that this unit observed and accepted the new generation.
		current.AppliedRevision = revision
		current.LastError, current.Candidate = "", nil
		if err := r.refreshRuntimeState(&current, id); err != nil {
			return false, err
		}
		actual.Units[id] = current
		return revisionChanged, nil
	}
	current := actual.Units[id]
	if current.Fingerprint != "" {
		if err := r.refreshRuntimeState(&current, id); err != nil {
			return false, err
		}
	}
	current.Candidate = &CandidateState{Fingerprint: fingerprint, Phase: PhaseUninstalled, PhaseChangedAt: r.now()}
	if current.Fingerprint == "" {
		if err := r.setUnitPhase(&current, PhaseUninstalled); err != nil {
			return false, err
		}
	}
	actual.Units[id] = current
	if err := r.checkpoint(actual); err != nil {
		return false, err
	}

	installed, stage, err := r.prepare(ctx, unit)
	r.pulse()
	if err != nil {
		return false, r.recordCandidateFailure(actual, id, stage, err)
	}
	current = actual.Units[id]
	current.Candidate.Plugins = installed
	if err := r.setCandidatePhase(&current, PhaseInstalledInactive); err != nil {
		return false, err
	}
	actual.Units[id] = current
	if err := r.checkpoint(actual); err != nil {
		return false, err
	}
	migrations, err := planStateMigrations(id, fingerprint, current.Plugins, installed)
	if err != nil {
		return false, r.recordCandidateFailure(actual, id, "migration_contract", err)
	}
	if err := r.setCandidatePhase(&current, PhaseActivating); err != nil {
		return false, err
	}
	actual.Units[id] = current
	if err := r.checkpoint(actual); err != nil {
		return false, err
	}
	envelope, err := configEnvelope(unit.Config, unit.Plugins)
	if err != nil {
		return false, r.recordCandidateFailure(actual, id, "configuration", err)
	}
	runtimeUnit := RuntimeUnit{
		ID: id, Fingerprint: fingerprint, ServiceRole: unit.ServiceRole,
		LogicalService: unit.LogicalService, InstancePolicy: policy.InstancePolicy,
		StateModel: policy.StateModel, Visibility: policy.Visibility, Routing: policy.Routing,
		RoutingDomain:         policy.RoutingDomain,
		PartitionKeys:         envelope.PartitionKeys,
		EnvironmentAllowlists: envelope.EnvironmentAllowlist,
		Config:                RawConfig(unit.Config), Plugins: installed, Migrations: migrations,
		RestartBase: current.RestartCount,
	}
	if err := r.Runtime.Apply(ctx, runtimeUnit); err != nil {
		return false, r.recordCandidateFailure(actual, id, runtimeFailureStage(err), err)
	}
	state := UnitState{
		Fingerprint: fingerprint, AppliedRevision: revision,
		Phase: PhaseActive, PhaseChangedAt: r.now(), Plugins: installed,
	}
	if err := r.refreshRuntimeState(&state, id); err != nil {
		return false, err
	}
	actual.Units[id] = state
	return true, r.checkpoint(actual)
}

func runtimeFailureStage(err error) string {
	var migrationErr *StateMigrationError
	if errors.As(err, &migrationErr) {
		return "migration_" + migrationErr.Phase
	}
	return "launch"
}

func (r *Reconciler) recordCandidateFailure(actual *ActualState, id, stage string, cause error) error {
	actual.Errors = append(actual.Errors, OperationError{UnitID: id, Stage: stage, Message: cause.Error()})
	current := actual.Units[id]
	if err := r.failCandidate(&current, id, cause); err != nil {
		return err
	}
	actual.Units[id] = current
	return r.checkpoint(actual)
}
