package pluginlibrarysource

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/engineering/internal/plugindev"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
)

type memoryStateStore struct {
	state State
	saved bool
}

func (s *memoryStateStore) Load() (State, bool, error) { return s.state, s.saved, nil }
func (s *memoryStateStore) Save(state State) error {
	s.state, s.saved = state, true
	return nil
}

type fakeBuilder struct{ built []plugindev.Candidate }

func (b *fakeBuilder) Build(_ context.Context, spec plugindev.Spec, digest string, generation uint64) (plugindev.Candidate, error) {
	version, err := plugindev.WorkspaceVersion(spec.Version, digest[:16])
	if err != nil {
		return plugindev.Candidate{}, err
	}
	candidate := plugindev.Candidate{PluginID: spec.ID, Version: version, SourceDigest: digest, Generation: generation}
	b.built = append(b.built, candidate)
	return candidate, nil
}

type fakePublisher struct{ published []plugindev.Candidate }

func (p *fakePublisher) Publish(_ context.Context, candidate plugindev.Candidate) error {
	p.published = append(p.published, candidate)
	return nil
}

type fakeWithdrawer struct{ refs []pluginv1.ArtifactRef }

func (w *fakeWithdrawer) WithdrawWorkspace(_ context.Context, ref pluginv1.ArtifactRef) error {
	w.refs = append(w.refs, ref)
	return nil
}

func (*fakeWithdrawer) WorkspaceCandidates(context.Context, string) ([]artifactrepositoryv1.Receipt, error) {
	return nil, nil
}

type fakeIntentApplier struct{ intents []InstallationIntent }

func (a *fakeIntentApplier) ApplyInstallationIntent(_ context.Context, intent InstallationIntent) error {
	a.intents = append(a.intents, intent)
	return nil
}

func TestControllerPublishesUpdatesThenWithdrawsDeletedSource(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "extensions", "plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"contracts", "core/shared/go", "extensions/sdk/go"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(directory)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, filename := range []string{"go.mod", "go.sum"} {
		if err := os.WriteFile(filepath.Join(root, filename), []byte("test\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC)
	store, builder, publisher, withdrawer, applier := &memoryStateStore{}, &fakeBuilder{}, &fakePublisher{}, &fakeWithdrawer{}, &fakeIntentApplier{}
	controller := &Controller{
		RepositoryRoot: root, Debounce: time.Second, Builder: builder, Publisher: publisher,
		Withdrawer: withdrawer, IntentApplier: applier, Store: store, Now: func() time.Time { return now }, Logf: func(string, ...any) {}, pending: map[string]pendingChange{},
	}
	initial, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	state := controller.initialState(initial)

	sourceID := "extensions/plugins/cn.vastplan.platform.testing.source-test"
	directory := filepath.Join(root, filepath.FromSlash(sourceID))
	writeSourcePlugin(t, directory, "")
	observed, _ := Scan(root)
	if err := controller.reconcile(context.Background(), &state, observed); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if err := controller.reconcile(context.Background(), &state, observed); err != nil {
		t.Fatal(err)
	}
	first := state.Sources[sourceID].ActiveRef
	if first == nil || len(publisher.published) != 1 || len(withdrawer.refs) != 0 {
		t.Fatalf("新增未形成 workspace 候选: state=%+v published=%d withdrawn=%d", state.Sources[sourceID], len(publisher.published), len(withdrawer.refs))
	}
	if len(applier.intents) != 1 || applier.intents[0].Action != plugininstallation.ActionInstall || applier.intents[0].Artifact == nil || *applier.intents[0].Artifact != *first {
		t.Fatalf("新增源码必须形成安装意图: %+v", applier.intents)
	}

	writeSourcePlugin(t, directory, "updated")
	observed, _ = Scan(root)
	controller.reconcile(context.Background(), &state, observed)
	now = now.Add(2 * time.Second)
	if err := controller.reconcile(context.Background(), &state, observed); err != nil {
		t.Fatal(err)
	}
	second := state.Sources[sourceID].ActiveRef
	if second == nil || *second == *first || len(publisher.published) != 2 || len(withdrawer.refs) != 1 || withdrawer.refs[0] != *first {
		t.Fatalf("更新必须先发布新候选再撤回旧候选: state=%+v published=%d withdrawn=%+v", state.Sources[sourceID], len(publisher.published), withdrawer.refs)
	}
	if len(applier.intents) != 2 || applier.intents[1].Action != plugininstallation.ActionUpgrade || applier.intents[1].Artifact == nil || *applier.intents[1].Artifact != *second {
		t.Fatalf("源码更新必须形成升级意图: %+v", applier.intents)
	}

	if err := os.RemoveAll(directory); err != nil {
		t.Fatal(err)
	}
	observed, _ = Scan(root)
	controller.reconcile(context.Background(), &state, observed)
	now = now.Add(2 * time.Second)
	if err := controller.reconcile(context.Background(), &state, observed); err != nil {
		t.Fatal(err)
	}
	if state.Sources[sourceID].Phase != PhaseRemoved || state.Sources[sourceID].ActiveRef != nil || len(withdrawer.refs) != 2 || withdrawer.refs[1] != *second {
		t.Fatalf("删除未撤回当前 workspace 候选: state=%+v withdrawn=%+v", state.Sources[sourceID], withdrawer.refs)
	}
	if len(applier.intents) != 3 || applier.intents[2].Action != plugininstallation.ActionRemove || applier.intents[2].Artifact != nil {
		t.Fatalf("源码删除必须形成卸载意图: %+v", applier.intents)
	}
}

func TestControllerRetriesPersistedFailureAfterRestart(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "extensions", "plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"contracts", "core/shared/go", "extensions/sdk/go"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(directory)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, filename := range []string{"go.mod", "go.sum"} {
		if err := os.WriteFile(filepath.Join(root, filename), []byte("test\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	sourceID := "extensions/plugins/cn.vastplan.platform.testing.source-test"
	writeSourcePlugin(t, filepath.Join(root, filepath.FromSlash(sourceID)), "")
	observed, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	item := observed[sourceID]
	now := time.Date(2026, 8, 2, 1, 0, 0, 0, time.UTC)
	store, builder, publisher, withdrawer := &memoryStateStore{}, &fakeBuilder{}, &fakePublisher{}, &fakeWithdrawer{}
	controller := &Controller{
		RepositoryRoot: root, Debounce: time.Second, Builder: builder, Publisher: publisher,
		Withdrawer: withdrawer, Store: store, Now: func() time.Time { return now }, Logf: func(string, ...any) {}, pending: map[string]pendingChange{},
	}
	state := State{SchemaVersion: stateSchemaVersion, Initialized: true, Sources: map[string]SourceState{
		sourceID: {SourceID: sourceID, PluginID: item.Spec.ID, Fingerprint: item.Fingerprint, Phase: PhaseFailed, LastError: "旧契约拒绝"},
	}}

	retryFailedSources(&state)
	if err := controller.reconcile(context.Background(), &state, observed); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if err := controller.reconcile(context.Background(), &state, observed); err != nil {
		t.Fatal(err)
	}
	if state.Sources[sourceID].Phase != PhaseReady || len(publisher.published) != 1 {
		t.Fatalf("控制器重启后必须重试未变化的失败源码: state=%+v published=%d", state.Sources[sourceID], len(publisher.published))
	}
}

func writeSourcePlugin(t *testing.T, directory, marker string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"id":"cn.vastplan.platform.testing.source-test","name":"Source test","description":"Source test plugin","version":"1.0.0","publisher":"vastplan","engines":{"backend":"^0.1"},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}}`)
	if err := os.WriteFile(filepath.Join(directory, "vastplan.plugin.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "content.txt"), []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(directory, "backend"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "backend", "main.go"), []byte("package main\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
