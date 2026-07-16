package nodeagent

import (
	"context"
	"errors"
	"testing"
	"time"

	deploymentv1 "cdsoft.com.cn/VastPlan/schemas/deployment/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
)

type fakeRepository struct{}

func (fakeRepository) Read(ref pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, error) {
	if ref.Version == "9.9.9" {
		return pluginv1.Artifact{}, nil, errors.New("制品不存在")
	}
	return pluginv1.Artifact{PluginID: ref.PluginID, Version: ref.Version, Channel: ref.Channel, SHA256: ref.Version, Size: 1}, []byte{1}, nil
}

type fakeInstaller struct{}

func (fakeInstaller) Install(a pluginv1.Artifact, _ []byte) (InstalledPlugin, error) {
	return InstalledPlugin{ID: a.PluginID, Version: a.Version, Channel: a.Channel, SHA256: a.SHA256, EntryPath: "/" + a.Version}, nil
}

type statefulInstaller struct {
	allowV2FromV1 bool
}

func (i statefulInstaller) Install(a pluginv1.Artifact, _ []byte) (InstalledPlugin, error) {
	plugin := InstalledPlugin{ID: a.PluginID, Version: a.Version, Channel: a.Channel, SHA256: a.SHA256, EntryPath: "/" + a.Version}
	switch a.Version {
	case "1.0.0":
		plugin.State = &PluginStateContract{PluginStateIdentity: PluginStateIdentity{Format: "com.example.demo.state", FormatVersion: 1}}
	case "2.0.0":
		plugin.State = &PluginStateContract{PluginStateIdentity: PluginStateIdentity{Format: "com.example.demo.state", FormatVersion: 2}}
		if i.allowV2FromV1 {
			plugin.State.MigrationProtocol = stateMigrationProtocolV1
			plugin.State.MigrationFrom = []PluginStateIdentity{{Format: "com.example.demo.state", FormatVersion: 1}}
		}
	}
	return plugin, nil
}

type fakeRuntime struct {
	units      map[string]RuntimeUnit
	applyCalls int
	stopCalls  int
	failEntry  string
	applyErr   error
	events     chan RuntimeEvent
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{units: map[string]RuntimeUnit{}, events: make(chan RuntimeEvent, 8)}
}

func (r *fakeRuntime) Apply(_ context.Context, unit RuntimeUnit) error {
	r.applyCalls++
	if r.applyErr != nil {
		return r.applyErr
	}
	for _, plugin := range unit.Plugins {
		if plugin.EntryPath == r.failEntry {
			return errors.New("候选进程启动失败")
		}
	}
	r.units[unit.ID] = unit
	return nil
}

func (r *fakeRuntime) Stop(_ context.Context, id string) error {
	r.stopCalls++
	delete(r.units, id)
	return nil
}

func (r *fakeRuntime) IsRunning(id, fingerprint string) bool {
	unit, ok := r.units[id]
	return ok && unit.Fingerprint == fingerprint
}

func (r *fakeRuntime) Status(id string) (RuntimeStatus, bool) {
	unit, ok := r.units[id]
	if !ok {
		return RuntimeStatus{}, false
	}
	return RuntimeStatus{
		Healthy: true, PIDs: []int{1234},
		StartedAt: time.Date(2026, 7, 16, 1, 0, 0, 0, time.UTC), RestartCount: unit.RestartBase,
	}, true
}

func (r *fakeRuntime) Events() <-chan RuntimeEvent { return r.events }

func (r *fakeRuntime) UnitIDs() []string {
	ids := make([]string, 0, len(r.units))
	for id := range r.units {
		ids = append(ids, id)
	}
	return ids
}

func (r *fakeRuntime) Close() error { return nil }

func desired(revision uint64, version string, enabled bool) deploymentv1.DesiredState {
	return deploymentv1.DesiredState{
		Version: 1, Revision: revision, Metadata: deploymentv1.Metadata{Name: "test"},
		Units: []deploymentv1.Unit{{
			ID: "backend-main", Kind: "service", Enabled: enabled, ServiceRole: "backend", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{{ID: "com.example.demo", Version: version, Channel: "stable"}},
		}},
	}
}

func newTestReconciler(runtime *fakeRuntime, store StateStore) *Reconciler {
	return &Reconciler{
		NodeID: "node-1", Repository: fakeRepository{}, Installer: fakeInstaller{}, Runtime: runtime, StateStore: store,
		Now: func() time.Time { return time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC) },
	}
}

func TestReconcile_IdempotentAndDisable(t *testing.T) {
	runtime := newFakeRuntime()
	store := NewMemoryStateStore()
	r := newTestReconciler(runtime, store)

	first, err := r.Reconcile(context.Background(), desired(1, "1.0.0", true))
	if err != nil || !first.Changed || !first.Converged {
		t.Fatalf("首次 reconcile = %+v, %v", first, err)
	}
	second, err := r.Reconcile(context.Background(), desired(1, "1.0.0", true))
	if err != nil || second.Changed || runtime.applyCalls != 1 {
		t.Fatalf("相同期望态必须幂等: result=%+v calls=%d err=%v", second, runtime.applyCalls, err)
	}
	disabled, err := r.Reconcile(context.Background(), desired(2, "1.0.0", false))
	if err != nil || !disabled.Changed || len(disabled.State.Units) != 0 || runtime.stopCalls != 1 {
		t.Fatalf("禁用应停止 unit: result=%+v stopCalls=%d err=%v", disabled, runtime.stopCalls, err)
	}
}

func TestReconcile_FailedUpgradePreservesOldAndRollbackConverges(t *testing.T) {
	runtime := newFakeRuntime()
	store := NewMemoryStateStore()
	r := newTestReconciler(runtime, store)
	baseline := desired(1, "1.0.0", true)
	if _, err := r.Reconcile(context.Background(), baseline); err != nil {
		t.Fatal(err)
	}
	oldFingerprint := runtime.units["backend-main"].Fingerprint

	runtime.failEntry = "/2.0.0"
	failed, err := r.Reconcile(context.Background(), desired(2, "2.0.0", true))
	if err == nil || failed.Converged || len(failed.State.Errors) != 1 {
		t.Fatalf("失败升级应报告未收敛: result=%+v err=%v", failed, err)
	}
	if got := runtime.units["backend-main"].Fingerprint; got != oldFingerprint {
		t.Fatalf("失败升级覆盖了旧实例: got %s want %s", got, oldFingerprint)
	}
	if failed.State.AppliedRevision != 1 || failed.State.Units["backend-main"].Fingerprint != oldFingerprint {
		t.Fatalf("失败升级覆盖了已应用状态: %+v", failed.State)
	}

	rolledBack, err := r.Reconcile(context.Background(), baseline)
	if err != nil || !rolledBack.Converged || rolledBack.Changed {
		t.Fatalf("回滚到仍在运行的组合应无重启收敛: result=%+v err=%v", rolledBack, err)
	}
	if rolledBack.State.AppliedRevision != 1 || len(rolledBack.State.Errors) != 0 {
		t.Fatalf("回滚状态不正确: %+v", rolledBack.State)
	}
}

func TestReconcile_StateMigrationPlanAndContractFailure(t *testing.T) {
	runtime := newFakeRuntime()
	store := NewMemoryStateStore()
	r := newTestReconciler(runtime, store)
	r.Installer = statefulInstaller{allowV2FromV1: true}
	if _, err := r.Reconcile(context.Background(), desired(1, "1.0.0", true)); err != nil {
		t.Fatal(err)
	}
	upgraded, err := r.Reconcile(context.Background(), desired(2, "2.0.0", true))
	if err != nil || !upgraded.Converged {
		t.Fatalf("声明完整的状态迁移应收敛: result=%+v err=%v", upgraded, err)
	}
	runtimeUnit := runtime.units["backend-main"]
	if len(runtimeUnit.Migrations) != 1 || runtimeUnit.Migrations[0].From.FormatVersion != 1 ||
		runtimeUnit.Migrations[0].To.FormatVersion != 2 || runtimeUnit.Migrations[0].TransactionID == "" {
		t.Fatalf("Reconciler 未把状态迁移计划交给 Runtime: %+v", runtimeUnit.Migrations)
	}

	// 从 v2 回到未声明任何 state 的 v9.9.9 会先在下载阶段失败，改用一个可下载的
	// 1.0.0 并移除迁移来源，证明契约检查发生在 Runtime.Apply 之前。
	r.Installer = statefulInstaller{allowV2FromV1: false}
	failed, err := r.Reconcile(context.Background(), desired(3, "1.0.0", true))
	if err == nil || failed.Converged {
		t.Fatal("未声明 v2 -> v1 来源的状态回退必须 fail-closed")
	}
	if runtime.applyCalls != 2 || runtime.units["backend-main"].Fingerprint != runtimeUnit.Fingerprint {
		t.Fatalf("迁移契约失败仍触发了 Runtime 或覆盖旧实例: calls=%d unit=%+v", runtime.applyCalls, runtime.units["backend-main"])
	}
	state := failed.State.Units["backend-main"]
	if state.Phase != PhaseActive || state.Candidate == nil || state.Candidate.Phase != PhaseFailed ||
		len(failed.State.Errors) != 1 || failed.State.Errors[0].Stage != "migration_contract" {
		t.Fatalf("迁移契约失败实际态不完整: %+v", failed.State)
	}
}

func TestReconcile_StateMigrationExecutionErrorIsDiagnosable(t *testing.T) {
	runtime := newFakeRuntime()
	r := newTestReconciler(runtime, NewMemoryStateStore())
	if _, err := r.Reconcile(context.Background(), desired(1, "1.0.0", true)); err != nil {
		t.Fatal(err)
	}
	runtime.applyErr = &StateMigrationError{PluginID: "com.example.demo", Phase: "commit", Err: errors.New("事务提交失败")}
	result, err := r.Reconcile(context.Background(), desired(2, "2.0.0", true))
	if err == nil || len(result.State.Errors) != 1 || result.State.Errors[0].Stage != "migration_commit" {
		t.Fatalf("迁移执行错误没有进入稳定诊断阶段: result=%+v err=%v", result, err)
	}
	state := result.State.Units["backend-main"]
	if state.Phase != PhaseActive || state.Candidate == nil || state.Candidate.Phase != PhaseFailed {
		t.Fatalf("迁移执行失败没有保留旧实例和失败候选: %+v", state)
	}
}

func TestReconcile_RejectsSameRevisionWithDifferentContent(t *testing.T) {
	runtime := newFakeRuntime()
	r := newTestReconciler(runtime, NewMemoryStateStore())
	if _, err := r.Reconcile(context.Background(), desired(7, "1.0.0", true)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(context.Background(), desired(7, "2.0.0", true)); err == nil {
		t.Fatal("同 revision 不同内容必须 fail-closed")
	}
}

func TestReconcile_NodeSelectorExcludesUnit(t *testing.T) {
	runtime := newFakeRuntime()
	r := newTestReconciler(runtime, NewMemoryStateStore())
	state := desired(1, "1.0.0", true)
	state.Units[0].Placement.NodeSelector = map[string]string{"zone": "west"}
	r.NodeLabels = map[string]string{"zone": "east"}
	result, err := r.Reconcile(context.Background(), state)
	if err != nil || !result.Converged || len(runtime.units) != 0 {
		t.Fatalf("不匹配节点不应启动 unit: result=%+v err=%v", result, err)
	}
}

func TestShutdown_StopsRuntimeAndUpdatesActualState(t *testing.T) {
	runtime := newFakeRuntime()
	store := NewMemoryStateStore()
	r := newTestReconciler(runtime, store)
	if _, err := r.Reconcile(context.Background(), desired(4, "1.0.0", true)); err != nil {
		t.Fatal(err)
	}
	if err := r.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	actual, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.units) != 0 || actual.AppliedRevision != 0 || actual.Units["backend-main"].Phase != PhaseInstalledInactive {
		t.Fatalf("优雅退出实际态不正确: runtime=%v actual=%+v", runtime.units, actual)
	}
}
