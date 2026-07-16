package nodeagent

import (
	"context"
	"slices"
	"testing"
)

func TestLifecycleTransitionGraph(t *testing.T) {
	legal := [][2]UnitPhase{
		{"", PhaseUninstalled},
		{PhaseUninstalled, PhaseInstalledInactive},
		{PhaseInstalledInactive, PhaseActivating},
		{PhaseActivating, PhaseActive},
		{PhaseActive, PhaseDraining},
		{PhaseDraining, PhaseDeactivating},
		{PhaseDeactivating, PhaseInstalledInactive},
		{PhaseDeactivating, PhaseRemoved},
		{PhaseFailed, PhaseUninstalled},
		{PhaseActive, PhaseActive},
	}
	for _, transition := range legal {
		if err := transitionPhase(transition[0], transition[1]); err != nil {
			t.Errorf("合法转换 %q -> %q 被拒绝: %v", transition[0], transition[1], err)
		}
	}
	illegal := [][2]UnitPhase{
		{PhaseUninstalled, PhaseActive},
		{PhaseActive, PhaseRemoved},
		{PhaseRemoved, PhaseActive},
		{PhaseActivating, PhaseDraining},
		{PhaseActive, "invented"},
	}
	for _, transition := range illegal {
		if err := transitionPhase(transition[0], transition[1]); err == nil {
			t.Errorf("非法转换 %q -> %q 未被拒绝", transition[0], transition[1])
		}
	}
}

type recordingStateStore struct {
	state     ActualState
	snapshots []ActualState
}

func newRecordingStateStore() *recordingStateStore {
	return &recordingStateStore{state: emptyActualState()}
}

func (s *recordingStateStore) Load() (ActualState, error) {
	return cloneState(s.state), nil
}

func (s *recordingStateStore) Save(state ActualState) error {
	if err := validateActualState(state); err != nil {
		return err
	}
	s.state = cloneState(state)
	s.snapshots = append(s.snapshots, cloneState(state))
	return nil
}

func (s *recordingStateStore) phases(unitID string) []UnitPhase {
	phases := make([]UnitPhase, 0, len(s.snapshots))
	for _, snapshot := range s.snapshots {
		if state, ok := snapshot.Units[unitID]; ok {
			phase := state.Phase
			if state.Candidate != nil {
				phase = state.Candidate.Phase
			}
			if len(phases) == 0 || phases[len(phases)-1] != phase {
				phases = append(phases, phase)
			}
		}
	}
	return phases
}

func TestReconcilePersistsLifecycleCheckpoints(t *testing.T) {
	runtime := newFakeRuntime()
	store := newRecordingStateStore()
	r := newTestReconciler(runtime, store)
	if _, err := r.Reconcile(context.Background(), desired(1, "1.0.0", true)); err != nil {
		t.Fatal(err)
	}
	wantInstall := []UnitPhase{PhaseUninstalled, PhaseInstalledInactive, PhaseActivating, PhaseActive}
	if got := store.phases("backend-main"); !slices.Equal(got, wantInstall) {
		t.Fatalf("首次安装状态序列 = %v，期望 %v", got, wantInstall)
	}

	store.snapshots = nil
	if _, err := r.Reconcile(context.Background(), desired(2, "1.0.0", false)); err != nil {
		t.Fatal(err)
	}
	wantRemove := []UnitPhase{PhaseDraining, PhaseDeactivating, PhaseRemoved}
	if got := store.phases("backend-main"); !slices.Equal(got, wantRemove) {
		t.Fatalf("移除状态序列 = %v，期望 %v", got, wantRemove)
	}
}

func TestFailedUpgradeReportsCandidateWithoutOverwritingCurrent(t *testing.T) {
	runtime := newFakeRuntime()
	store := newRecordingStateStore()
	r := newTestReconciler(runtime, store)
	if _, err := r.Reconcile(context.Background(), desired(1, "1.0.0", true)); err != nil {
		t.Fatal(err)
	}
	baseline := store.state.Units["backend-main"]
	runtime.failEntry = "/2.0.0"
	result, err := r.Reconcile(context.Background(), desired(2, "2.0.0", true))
	if err == nil {
		t.Fatal("候选启动失败必须让本轮对账失败")
	}
	state := result.State.Units["backend-main"]
	if state.Fingerprint != baseline.Fingerprint || state.Phase != PhaseActive {
		t.Fatalf("旧实例被候选失败覆盖: baseline=%+v actual=%+v", baseline, state)
	}
	if state.Candidate == nil || state.Candidate.Phase != PhaseFailed || state.Candidate.LastError == "" {
		t.Fatalf("候选失败实际态不完整: %+v", state.Candidate)
	}
	wantCandidate := []UnitPhase{PhaseUninstalled, PhaseInstalledInactive, PhaseActivating, PhaseFailed}
	store.snapshots = store.snapshots[len(store.snapshots)-5:]
	if got := store.phases("backend-main"); !slices.Equal(got, wantCandidate) {
		t.Fatalf("升级候选状态序列 = %v，期望 %v", got, wantCandidate)
	}
}
