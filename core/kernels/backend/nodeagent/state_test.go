package nodeagent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStateStoreMigratesV1ToCurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actual.json")
	raw := []byte(`{
  "version": 1,
  "node_id": "node-1",
  "observed_revision": 7,
  "applied_revision": 6,
  "updated_at": "2026-07-16T00:00:00Z",
  "units": {
    "backend-main": {
      "fingerprint": "old",
      "applied_revision": 6,
      "status": "running",
      "plugins": [],
      "restart_count": 2
    }
  }
}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	store := FileStateStore{Path: path}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	unit := state.Units["backend-main"]
	if state.Version != actualStateVersion || unit.Phase != PhaseActive || unit.RestartCount != 2 {
		t.Fatalf("v1 迁移结果不正确: %+v", state)
	}
	if want := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC); !unit.PhaseChangedAt.Equal(want) {
		t.Fatalf("迁移状态时间 = %s，期望 %s", unit.PhaseChangedAt, want)
	}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Version != actualStateVersion || reloaded.Units["backend-main"].Phase != PhaseActive {
		t.Fatalf("当前版本回写后不可重读: %+v", reloaded)
	}
}

func TestDecodeActualStateMigratesV3ToV4(t *testing.T) {
	state, err := decodeActualState([]byte(`{"version":3,"node_id":"node-1","units":{},"updated_at":"2026-07-21T00:00:00Z"}`))
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != actualStateVersion || state.BootstrapGeneration != 0 || !state.BootstrapPublishedAt.IsZero() {
		t.Fatalf("v3 实际态未安全迁移到 v4: %+v", state)
	}
}

func TestActualStateRejectsUnknownPhaseAndVersion(t *testing.T) {
	state := emptyActualState()
	state.Units["bad"] = UnitState{Phase: "running"}
	if err := validateActualState(state); err == nil {
		t.Fatal("未知 phase 必须 fail-closed")
	}
	state = emptyActualState()
	state.Version = 99
	if err := validateActualState(state); err == nil {
		t.Fatal("未知实际态版本必须 fail-closed")
	}
}
