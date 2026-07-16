package nodeagent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"cdsoft.com.cn/VastPlan/kernels/backend/pluginservice"
	deploymentv1 "cdsoft.com.cn/VastPlan/schemas/deployment/v1"
)

// Reconciler 把一份完整期望态收敛到当前节点。
type Reconciler struct {
	NodeID     string
	NodeLabels map[string]string
	Repository ArtifactRepository
	Installer  Installer
	Runtime    Runtime
	StateStore StateStore
	Now        func() time.Time
}

// Reconcile 每次执行都是幂等的。候选插件全部安装且启动成功后 Runtime 才替换旧实例；
// 任一阶段失败会保留旧 UnitState，并把错误写入 ActualState。
func (r *Reconciler) Reconcile(ctx context.Context, desired deploymentv1.DesiredState) (Result, error) {
	if err := r.validate(); err != nil {
		return Result{}, err
	}
	actual, err := r.StateStore.Load()
	if err != nil {
		return Result{}, err
	}
	if actual.NodeID != "" && actual.NodeID != r.NodeID {
		return Result{}, fmt.Errorf("实际态属于节点 %q，当前节点为 %q", actual.NodeID, r.NodeID)
	}
	digest := desired.Digest()
	if actual.ObservedRevision == desired.Revision && actual.ObservedDigest != "" && actual.ObservedDigest != digest {
		return Result{}, fmt.Errorf("revision %d 的期望态内容发生冲突", desired.Revision)
	}
	actual.Version = 1
	actual.NodeID = r.NodeID
	actual.ObservedRevision = desired.Revision
	actual.ObservedDigest = digest
	actual.Errors = nil

	targets := make(map[string]deploymentv1.Unit)
	for _, unit := range desired.Units {
		if unit.Enabled && unit.MatchesNode(r.NodeLabels) {
			targets[unit.ID] = unit
		}
	}
	ids := sortedUnitIDs(targets)
	changed := false
	for _, id := range ids {
		unit := targets[id]
		fingerprint := unit.Fingerprint()
		if current, ok := actual.Units[id]; ok && current.Fingerprint == fingerprint && r.Runtime.IsRunning(id, fingerprint) {
			current.Status = "running"
			current.LastError = ""
			r.refreshRuntimeState(&current, id)
			actual.Units[id] = current
			continue
		}
		current := actual.Units[id]
		installed, stage, installErr := r.prepare(unit)
		if installErr != nil {
			actual.Errors = append(actual.Errors, OperationError{UnitID: id, Stage: stage, Message: installErr.Error()})
			current.Status = "degraded"
			current.LastError = installErr.Error()
			r.refreshRuntimeState(&current, id)
			if current.Fingerprint != "" {
				actual.Units[id] = current
			}
			continue
		}
		runtimeUnit := RuntimeUnit{
			ID: id, Fingerprint: fingerprint, ServiceRole: unit.ServiceRole,
			Config: RawConfig(unit.Config), Plugins: installed, RestartBase: current.RestartCount,
		}
		if err := r.Runtime.Apply(ctx, runtimeUnit); err != nil {
			actual.Errors = append(actual.Errors, OperationError{UnitID: id, Stage: "launch", Message: err.Error()})
			current.Status = "degraded"
			current.LastError = err.Error()
			r.refreshRuntimeState(&current, id)
			if current.Fingerprint != "" {
				actual.Units[id] = current
			}
			continue
		}
		state := UnitState{Fingerprint: fingerprint, AppliedRevision: desired.Revision, Status: "running", Plugins: installed}
		r.refreshRuntimeState(&state, id)
		actual.Units[id] = state
		changed = true
	}

	for _, id := range unionUnitIDs(actual.Units, r.Runtime.UnitIDs()) {
		if _, keep := targets[id]; keep {
			continue
		}
		if err := r.Runtime.Stop(ctx, id); err != nil {
			actual.Errors = append(actual.Errors, OperationError{UnitID: id, Stage: "stop", Message: err.Error()})
			continue
		}
		delete(actual.Units, id)
		changed = true
	}

	converged := len(actual.Errors) == 0
	if converged {
		for id, unit := range targets {
			fingerprint := unit.Fingerprint()
			state, ok := actual.Units[id]
			if !ok || state.Fingerprint != fingerprint || !r.Runtime.IsRunning(id, fingerprint) {
				converged = false
				break
			}
		}
	}
	if converged {
		actual.AppliedRevision = desired.Revision
	}
	actual.UpdatedAt = r.now()
	if err := r.StateStore.Save(actual); err != nil {
		return Result{}, err
	}
	if converged {
		if collector, ok := r.Installer.(GarbageCollector); ok {
			if err := collector.GarbageCollect(referencedSHA256(actual)); err != nil {
				actual.Errors = append(actual.Errors, OperationError{Stage: "gc", Message: err.Error()})
				actual.UpdatedAt = r.now()
				_ = r.StateStore.Save(actual)
				return Result{Changed: changed, Converged: false, State: actual}, fmt.Errorf("安装目录垃圾回收失败: %w", err)
			}
		}
	}
	result := Result{Changed: changed, Converged: converged, State: actual}
	if !converged {
		return result, fmt.Errorf("节点 %s 未收敛：%d 个操作失败", r.NodeID, len(actual.Errors))
	}
	return result, nil
}

func (r *Reconciler) refreshRuntimeState(state *UnitState, unitID string) {
	status, ok := r.Runtime.Status(unitID)
	if !ok {
		state.PIDs = nil
		state.StartedAt = nil
		return
	}
	state.PIDs = append(state.PIDs[:0], status.PIDs...)
	startedAt := status.StartedAt
	state.StartedAt = &startedAt
	state.RestartCount = status.RestartCount
	if !status.Healthy && state.Status == "running" {
		state.Status = "degraded"
	}
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

// Shutdown 在 Node Agent 优雅退出时停止本进程管理的 unit，并把本地报告改为 stopped。
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
		if err := r.Runtime.Stop(ctx, id); err != nil {
			actual.Errors = append(actual.Errors, OperationError{UnitID: id, Stage: "shutdown", Message: err.Error()})
			continue
		}
		if state, ok := actual.Units[id]; ok {
			state.Status = "stopped"
			actual.Units[id] = state
		}
	}
	// 进程退出后，即使实际态里留有历史 unit，本节点也不再满足当前期望态。
	actual.AppliedRevision = 0
	actual.UpdatedAt = r.now()
	if err := r.StateStore.Save(actual); err != nil {
		return err
	}
	if len(actual.Errors) > 0 {
		return fmt.Errorf("节点 %s 关闭时有 %d 个操作失败", r.NodeID, len(actual.Errors))
	}
	return nil
}

func (r *Reconciler) prepare(unit deploymentv1.Unit) ([]InstalledPlugin, string, error) {
	plugins := make([]InstalledPlugin, 0, len(unit.Plugins))
	for _, ref := range unit.Plugins {
		artifact, packageBytes, err := r.Repository.Read(pluginservice.Ref{PluginID: ref.ID, Version: ref.Version, Channel: ref.Channel})
		if err != nil {
			return nil, "download", fmt.Errorf("读取 %s@%s/%s: %w", ref.ID, ref.Version, ref.Channel, err)
		}
		installed, err := r.Installer.Install(artifact, packageBytes)
		if err != nil {
			return nil, "install", fmt.Errorf("安装 %s@%s/%s: %w", ref.ID, ref.Version, ref.Channel, err)
		}
		plugins = append(plugins, installed)
	}
	return plugins, "", nil
}

func (r *Reconciler) validate() error {
	if r.NodeID == "" {
		return errors.New("node id 不能为空")
	}
	if r.Repository == nil || r.Installer == nil || r.Runtime == nil || r.StateStore == nil {
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
