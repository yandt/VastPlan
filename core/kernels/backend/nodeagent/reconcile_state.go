package nodeagent

import (
	"context"
	"errors"
	"fmt"
	"sort"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
)

func (r *Reconciler) removeObsoleteUnits(ctx context.Context, targets map[string]deploymentv1.Unit, actual *ActualState) (bool, error) {
	changed := false
	for _, id := range unionUnitIDs(actual.Units, r.Runtime.UnitIDs()) {
		r.pulse()
		if _, keep := targets[id]; keep {
			continue
		}
		removed, err := r.removeUnit(ctx, actual, id)
		changed = changed || removed
		if err != nil {
			return changed, err
		}
	}
	return changed, nil
}

func (r *Reconciler) removeUnit(ctx context.Context, actual *ActualState, id string) (bool, error) {
	state, ok := actual.Units[id]
	if !ok {
		state = UnitState{Phase: PhaseActive, PhaseChangedAt: r.now()}
	}
	for _, phase := range []UnitPhase{PhaseDraining, PhaseDeactivating} {
		if err := r.setUnitPhase(&state, phase); err != nil {
			return false, err
		}
		actual.Units[id] = state
		if err := r.checkpoint(actual); err != nil {
			return false, err
		}
	}
	if err := r.Runtime.Stop(ctx, id); err != nil {
		actual.Errors = append(actual.Errors, OperationError{UnitID: id, Stage: "stop", Message: err.Error()})
		state.LastError = err.Error()
		if phaseErr := r.setUnitPhase(&state, PhaseFailed); phaseErr != nil {
			return false, phaseErr
		}
		actual.Units[id] = state
		return false, r.checkpoint(actual)
	}
	if err := r.setUnitPhase(&state, PhaseRemoved); err != nil {
		return false, err
	}
	actual.Units[id] = state
	if err := r.checkpoint(actual); err != nil {
		return true, err
	}
	delete(actual.Units, id)
	return true, nil
}

func (r *Reconciler) isConverged(targets map[string]deploymentv1.Unit, actual ActualState) bool {
	if len(actual.Errors) != 0 {
		return false
	}
	for id, unit := range targets {
		fingerprint := unit.Fingerprint()
		state, ok := actual.Units[id]
		if !ok || state.Fingerprint != fingerprint || !r.Runtime.IsRunning(id, fingerprint) {
			return false
		}
	}
	return true
}

func (r *Reconciler) refreshRuntimeState(state *UnitState, unitID string) error {
	status, ok := r.Runtime.Status(unitID)
	if !ok {
		state.PIDs = nil
		state.StartedAt = nil
		if state.Fingerprint != "" && state.Phase == PhaseActive {
			state.LastError = "运行时实例不存在"
			return r.setUnitPhase(state, PhaseFailed)
		}
		return nil
	}
	state.PIDs = append(state.PIDs[:0], status.PIDs...)
	startedAt := status.StartedAt
	state.StartedAt = &startedAt
	state.RestartCount = status.RestartCount
	state.Readiness = status.Readiness
	state.DependencyIssues = append(state.DependencyIssues[:0], status.DependencyIssues...)
	if !status.Healthy {
		state.LastError = "运行时健康检查失败"
		return r.setUnitPhase(state, PhaseFailed)
	}
	if state.Fingerprint != "" && state.Phase == PhaseFailed {
		state.LastError = ""
		return r.setUnitPhase(state, PhaseActive)
	}
	return nil
}

func (r *Reconciler) setUnitPhase(state *UnitState, phase UnitPhase) error {
	if err := transitionPhase(state.Phase, phase); err != nil {
		return err
	}
	if state.Phase != phase || state.PhaseChangedAt.IsZero() {
		state.Phase = phase
		state.PhaseChangedAt = r.now()
	}
	return nil
}

func (r *Reconciler) setCandidatePhase(state *UnitState, phase UnitPhase) error {
	if state.Candidate == nil {
		return errors.New("候选实际态不存在")
	}
	if err := transitionPhase(state.Candidate.Phase, phase); err != nil {
		return fmt.Errorf("候选状态: %w", err)
	}
	if state.Candidate.Phase != phase || state.Candidate.PhaseChangedAt.IsZero() {
		state.Candidate.Phase = phase
		state.Candidate.PhaseChangedAt = r.now()
	}
	// 首次安装没有稳定实例，顶层状态与候选同步；升级时顶层继续如实报告旧实例。
	if state.Fingerprint == "" {
		return r.setUnitPhase(state, phase)
	}
	return nil
}

func (r *Reconciler) failCandidate(state *UnitState, unitID string, cause error) error {
	if err := r.setCandidatePhase(state, PhaseFailed); err != nil {
		return err
	}
	state.Candidate.LastError = cause.Error()
	if state.Fingerprint != "" {
		if err := r.refreshRuntimeState(state, unitID); err != nil {
			return err
		}
	}
	return nil
}

// checkpoint 在长操作前后写入实际态，使控制面不仅能看到最终结果，也能看到
// 安装、激活、排空和停用等中间状态。写入失败时调用方必须停止后续副作用。
func (r *Reconciler) checkpoint(actual *ActualState) error {
	actual.Version = actualStateVersion
	actual.UpdatedAt = r.now()
	return r.StateStore.Save(*actual)
}

func referencedSHA256(actual ActualState) []string {
	set := map[string]struct{}{}
	for _, unit := range actual.Units {
		for _, plugin := range unit.Plugins {
			set[plugin.SHA256] = struct{}{}
		}
	}
	refs := make([]string, 0, len(set))
	for sha := range set {
		refs = append(refs, sha)
	}
	sort.Strings(refs)
	return refs
}

// Shutdown 在 Node Agent 优雅退出时按 draining -> deactivating -> installed_inactive
// 记录检查点并停止本进程管理的 unit。
// 它不修改 DesiredState；下一次启动会因运行时为空而重新装配仍启用的 unit。
