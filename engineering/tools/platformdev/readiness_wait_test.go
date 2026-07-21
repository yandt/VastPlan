package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWaitForUnitsRejectsReadyStateFromPriorProcess(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "actual-state.json")
	writeReadinessFixture(t, filename, time.Now().Add(-time.Hour), false)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := waitForUnits(ctx, filename, 1, time.Now(), 50*time.Millisecond); err == nil {
		t.Fatal("上一次进程留下的 Ready 状态不得满足本次启动门禁")
	}
}

func TestWaitForUnitsRejectsActiveUnitWithCandidate(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "actual-state.json")
	writeReadinessFixture(t, filename, time.Now().Add(time.Minute), true)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := waitForUnits(ctx, filename, 1, time.Now(), 50*time.Millisecond); err == nil {
		t.Fatal("仍在启动候选实例的 unit 不得满足收敛门禁")
	}
}

func TestWaitForUnitsAcceptsCurrentConvergedState(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "actual-state.json")
	startedAt := time.Now()
	writeReadinessFixture(t, filename, startedAt.Add(time.Second), false)

	if err := waitForUnits(context.Background(), filename, 1, startedAt, 50*time.Millisecond); err != nil {
		t.Fatalf("本次启动后形成的无候选 Ready 状态应通过门禁: %v", err)
	}
}

func TestUnitsReadyAfterStartRejectsUncommittedRevision(t *testing.T) {
	startedAt := time.Now()
	ready, summary := unitsReadyAfterStart(readinessState{
		ObservedRevision: 2,
		AppliedRevision:  1,
		UpdatedAt:        startedAt.Add(time.Second),
		Units: map[string]readinessUnit{
			"service-a": {Phase: "active", Readiness: "ready"},
		},
	}, 1, startedAt)
	if ready || summary == "" {
		t.Fatalf("外部收敛事务尚未提交时不得开放启动门禁: ready=%v summary=%q", ready, summary)
	}
}

func writeReadinessFixture(t *testing.T, filename string, updatedAt time.Time, candidate bool) {
	t.Helper()
	unit := map[string]any{"phase": "active", "readiness": "ready"}
	if candidate {
		unit["candidate"] = map[string]any{"phase": "activating"}
	}
	raw, err := json.Marshal(map[string]any{
		"observed_revision": 1,
		"applied_revision":  1,
		"updated_at":        updatedAt.UTC(),
		"units":             map[string]any{"service-a": unit},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
