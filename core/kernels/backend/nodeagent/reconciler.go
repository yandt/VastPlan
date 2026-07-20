package nodeagent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/core/shared/go/servicemodel"
)

// Reconciler 把一份完整期望态收敛到当前节点。
type Reconciler struct {
	NodeID     string
	NodeLabels map[string]string
	Sources    []ArtifactSource
	Verifier   ArtifactVerifier
	Installer  Installer
	Runtime    Runtime
	StateStore StateStore
	Now        func() time.Time
}

// Reconcile 每次执行都是幂等的。当前实例与候选实例分别持久化；候选插件全部安装且
// 启动成功后 Runtime 才替换旧实例，任一阶段失败都保留旧实例并留下候选失败实际态。
func (r *Reconciler) Reconcile(ctx context.Context, desired deploymentv1.DesiredState) (Result, error) {
	actual, err := r.beginReconcile(desired)
	if err != nil {
		return Result{}, err
	}
	targets := r.targetUnits(desired)
	changed, err := r.reconcileTargets(ctx, desired.Revision, targets, &actual)
	if err != nil {
		return Result{Changed: changed, State: actual}, err
	}
	removed, err := r.removeObsoleteUnits(ctx, targets, &actual)
	changed = changed || removed
	if err != nil {
		return Result{Changed: changed, State: actual}, err
	}

	converged := r.isConverged(targets, actual)
	if converged {
		actual.AppliedRevision = desired.Revision
	}
	if err := r.checkpoint(&actual); err != nil {
		return Result{}, err
	}
	if converged {
		if collector, ok := r.Installer.(GarbageCollector); ok {
			if err := collector.GarbageCollect(referencedSHA256(actual)); err != nil {
				actual.Errors = append(actual.Errors, OperationError{Stage: "gc", Message: err.Error()})
				_ = r.checkpoint(&actual)
				return Result{Changed: changed, Converged: false, State: actual}, fmt.Errorf("安装目录垃圾回收失败: %w", err)
			}
		}
	}
	result := Result{Changed: changed, Converged: converged, State: actual}
	if !converged {
		failure := fmt.Errorf("节点 %s 未收敛：%d 个操作失败", r.NodeID, len(actual.Errors))
		for _, operation := range actual.Errors {
			failure = errors.Join(failure, fmt.Errorf("unit=%s stage=%s: %s", operation.UnitID, operation.Stage, operation.Message))
		}
		return result, failure
	}
	return result, nil
}

func (r *Reconciler) beginReconcile(desired deploymentv1.DesiredState) (ActualState, error) {
	if err := r.validate(); err != nil {
		return ActualState{}, err
	}
	actual, err := r.StateStore.Load()
	if err != nil {
		return ActualState{}, err
	}
	if actual.NodeID != "" && actual.NodeID != r.NodeID {
		return ActualState{}, fmt.Errorf("实际态属于节点 %q，当前节点为 %q", actual.NodeID, r.NodeID)
	}
	digest := desired.Digest()
	if actual.ObservedRevision == desired.Revision && actual.ObservedDigest != "" && actual.ObservedDigest != digest {
		return ActualState{}, fmt.Errorf("revision %d 的期望态内容发生冲突", desired.Revision)
	}
	actual.Version = actualStateVersion
	actual.NodeID = r.NodeID
	actual.ObservedRevision = desired.Revision
	actual.ObservedDigest = digest
	actual.Errors = nil
	return actual, nil
}

func (r *Reconciler) targetUnits(desired deploymentv1.DesiredState) map[string]deploymentv1.Unit {
	targets := make(map[string]deploymentv1.Unit)
	for _, unit := range desired.Units {
		if unit.Enabled && unit.MatchesNode(r.NodeLabels) {
			targets[unit.ID] = unit
		}
	}
	return targets
}

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
		unitChanged, err := r.reconcileTarget(ctx, revision, targets[id], actual)
		changed = changed || unitChanged
		if err != nil {
			return changed, err
		}
	}
	return changed, nil
}

func (r *Reconciler) reconcileTarget(ctx context.Context, revision uint64, unit deploymentv1.Unit, actual *ActualState) (bool, error) {
	id, fingerprint := unit.ID, unit.Fingerprint()
	policy, err := unitPolicy(unit)
	if err != nil {
		return false, err
	}
	if current, ok := actual.Units[id]; ok && current.Fingerprint == fingerprint && r.Runtime.IsRunning(id, fingerprint) {
		if err := r.setUnitPhase(&current, PhaseActive); err != nil {
			return false, err
		}
		current.LastError, current.Candidate = "", nil
		if err := r.refreshRuntimeState(&current, id); err != nil {
			return false, err
		}
		actual.Units[id] = current
		return false, nil
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

func (r *Reconciler) removeObsoleteUnits(ctx context.Context, targets map[string]deploymentv1.Unit, actual *ActualState) (bool, error) {
	changed := false
	for _, id := range unionUnitIDs(actual.Units, r.Runtime.UnitIDs()) {
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
func (r *Reconciler) Shutdown(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	actual, err := r.StateStore.Load()
	if err != nil {
		return err
	}
	actual.Errors = nil
	for _, id := range r.Runtime.UnitIDs() {
		state, ok := actual.Units[id]
		if !ok {
			state = UnitState{Phase: PhaseActive, PhaseChangedAt: r.now()}
		}
		if err := r.setUnitPhase(&state, PhaseDraining); err != nil {
			return err
		}
		actual.Units[id] = state
		if err := r.checkpoint(&actual); err != nil {
			return err
		}
		if err := r.setUnitPhase(&state, PhaseDeactivating); err != nil {
			return err
		}
		actual.Units[id] = state
		if err := r.checkpoint(&actual); err != nil {
			return err
		}
		if err := r.Runtime.Stop(ctx, id); err != nil {
			actual.Errors = append(actual.Errors, OperationError{UnitID: id, Stage: "shutdown", Message: err.Error()})
			state.LastError = err.Error()
			if phaseErr := r.setUnitPhase(&state, PhaseFailed); phaseErr != nil {
				return phaseErr
			}
			actual.Units[id] = state
			if saveErr := r.checkpoint(&actual); saveErr != nil {
				return saveErr
			}
			continue
		}
		if err := r.setUnitPhase(&state, PhaseInstalledInactive); err != nil {
			return err
		}
		state.PIDs = nil
		state.StartedAt = nil
		actual.Units[id] = state
		if err := r.checkpoint(&actual); err != nil {
			return err
		}
	}
	// 进程退出后，即使实际态里留有历史 unit，本节点也不再满足当前期望态。
	actual.AppliedRevision = 0
	if err := r.checkpoint(&actual); err != nil {
		return err
	}
	if len(actual.Errors) > 0 {
		return fmt.Errorf("节点 %s 关闭时有 %d 个操作失败", r.NodeID, len(actual.Errors))
	}
	return nil
}

func (r *Reconciler) prepare(ctx context.Context, unit deploymentv1.Unit) ([]InstalledPlugin, string, error) {
	plugins := make([]InstalledPlugin, 0, len(unit.Plugins))
	for _, ref := range unit.Plugins {
		artifactRef := pluginv1.ArtifactRef{PluginID: ref.ID, Version: ref.Version, Channel: ref.Channel}
		verified, err := r.resolveArtifact(ctx, artifactRef)
		if err != nil {
			return nil, "download", fmt.Errorf("读取 %s@%s/%s: %w", ref.ID, ref.Version, ref.Channel, err)
		}
		installed, err := r.Installer.Install(verified)
		if err != nil {
			return nil, "install", fmt.Errorf("安装 %s@%s/%s: %w", ref.ID, ref.Version, ref.Channel, err)
		}
		plugins = append(plugins, installed)
	}
	return plugins, "", nil
}

func (r *Reconciler) resolveArtifact(ctx context.Context, ref pluginv1.ArtifactRef) (VerifiedArtifact, error) {
	var notFound error
	for _, source := range r.Sources {
		if source == nil {
			return VerifiedArtifact{}, errors.New("制品源不能为空")
		}
		envelope, err := source.Fetch(ctx, ref)
		if errors.Is(err, artifacttrust.ErrNotFound) {
			notFound = errors.Join(notFound, fmt.Errorf("%s: %w", sourceName(source), err))
			continue
		}
		if err != nil {
			return VerifiedArtifact{}, fmt.Errorf("制品源 %s 失败: %w", sourceName(source), err)
		}
		verified, err := r.Verifier.Verify(ref, envelope)
		if err != nil {
			// 来源一旦返回内容，任何格式、摘要或证明失败都是安全事件；不得换源掩盖。
			return VerifiedArtifact{}, fmt.Errorf("制品源 %s 返回不可信内容: %w", sourceName(source), err)
		}
		return verified, nil
	}
	if notFound != nil {
		return VerifiedArtifact{}, fmt.Errorf("所有制品源均无此制品: %w", notFound)
	}
	return VerifiedArtifact{}, errors.New("没有可用制品源")
}

func (r *Reconciler) validate() error {
	if r.NodeID == "" {
		return errors.New("node id 不能为空")
	}
	if len(r.Sources) == 0 || !r.Verifier.configured || r.Installer == nil || r.Runtime == nil || r.StateStore == nil {
		return errors.New("reconciler 依赖未完整配置")
	}
	return nil
}

func (r *Reconciler) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func sortedUnitIDs(units map[string]deploymentv1.Unit) []string {
	ids := make([]string, 0, len(units))
	for id := range units {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func unionUnitIDs(units map[string]UnitState, runtimeIDs []string) []string {
	set := make(map[string]struct{}, len(units)+len(runtimeIDs))
	for id := range units {
		set[id] = struct{}{}
	}
	for _, id := range runtimeIDs {
		set[id] = struct{}{}
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
