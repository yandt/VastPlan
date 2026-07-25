package plugindev

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type cancelFirstBuilder struct {
	started chan int
	mu      sync.Mutex
	count   int
}

func (b *cancelFirstBuilder) Build(ctx context.Context, spec Spec, digest string, generation uint64) (Candidate, error) {
	b.mu.Lock()
	b.count++
	count := b.count
	b.mu.Unlock()
	b.started <- count
	if count == 1 {
		<-ctx.Done()
		return Candidate{}, ctx.Err()
	}
	version, _ := WorkspaceVersion(spec.Version, digest[:16])
	return Candidate{PluginID: spec.ID, Version: version, SourceDigest: digest, PackageFile: "/tmp/candidate.tar.gz", Generation: generation}, nil
}

type recordingPublisher struct {
	published chan Candidate
}

func (p recordingPublisher) Publish(_ context.Context, candidate Candidate) error {
	p.published <- candidate
	return nil
}

type immediateBuilder struct{}

func (immediateBuilder) Build(_ context.Context, spec Spec, digest string, generation uint64) (Candidate, error) {
	version, _ := WorkspaceVersion(spec.Version, digest[:16])
	return Candidate{PluginID: spec.ID, Version: version, SourceDigest: digest, PackageFile: "/tmp/candidate.tar.gz", Generation: generation}, nil
}

type blockingPublisher struct {
	started chan Candidate
	release chan struct{}
}

func (p blockingPublisher) Publish(ctx context.Context, candidate Candidate) error {
	p.started <- candidate
	select {
	case <-p.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestControllerCancelsUncommittedBuildAndPublishesOnlyLatestDigest(t *testing.T) {
	root, spec := fixtureRepository(t)
	builder := &cancelFirstBuilder{started: make(chan int, 4)}
	publisher := recordingPublisher{published: make(chan Candidate, 2)}
	status := &MemoryStatusWriter{}
	controller := &Controller{
		RepositoryRoot: root, Spec: spec, Target: "managed-services/demo", PollInterval: 10 * time.Millisecond,
		Debounce: 20 * time.Millisecond, Builder: builder, Publisher: publisher, Status: status,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- controller.Run(ctx) }()
	select {
	case <-builder.started:
	case <-ctx.Done():
		t.Fatal("first build did not start")
	}
	source := filepath.Join(root, "extensions", "plugins", spec.ID, "backend", "main.go")
	if err := os.WriteFile(source, []byte("package main\nfunc main() { println(2) }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case candidate := <-publisher.published:
		latest, err := SourceDigest(root, spec)
		if err != nil || candidate.SourceDigest != latest || candidate.Generation != 2 {
			t.Fatalf("published stale candidate: candidate=%+v latest=%s err=%v", candidate, latest, err)
		}
		cancel()
	case <-ctx.Done():
		t.Fatal("latest candidate was not published")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	values := status.Snapshot()
	if len(values) == 0 || values[len(values)-1].Phase != PhaseReady {
		t.Fatalf("controller status did not converge: %+v", values)
	}
}

func TestControllerFinishesPublishTransactionThenQueuesLatestSource(t *testing.T) {
	root, spec := fixtureRepository(t)
	publisher := blockingPublisher{started: make(chan Candidate, 2), release: make(chan struct{}, 2)}
	status := &MemoryStatusWriter{}
	controller := &Controller{
		RepositoryRoot: root, Spec: spec, Target: "managed-services/demo", PollInterval: 10 * time.Millisecond,
		Debounce: 20 * time.Millisecond, Builder: immediateBuilder{}, Publisher: publisher, Status: status,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- controller.Run(ctx) }()
	first := <-publisher.started
	source := filepath.Join(root, "extensions", "plugins", spec.ID, "backend", "main.go")
	if err := os.WriteFile(source, []byte("package main\nfunc main() { println(3) }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	publisher.release <- struct{}{}
	second := <-publisher.started
	if second.Generation != first.Generation+1 || second.SourceDigest == first.SourceDigest {
		t.Fatalf("最新输入没有排队为下一事务: first=%+v second=%+v", first, second)
	}
	publisher.release <- struct{}{}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		values := status.Snapshot()
		if len(values) != 0 && values[len(values)-1].Phase == PhaseReady && values[len(values)-1].SourceDigest == second.SourceDigest {
			cancel()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("latest publish did not become ready")
}
