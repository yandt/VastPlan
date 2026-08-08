package nodeagent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileStateStoreLoadsProductionBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actual.json")
	raw := []byte(`{
  "version": 4,
  "node_id": "node-1",
  "observed_revision": 7,
  "applied_revision": 6,
  "updated_at": "2026-07-16T00:00:00Z",
  "units": {
    "backend-main": {
      "fingerprint": "old",
      "applied_revision": 6,
      "phase": "active",
      "phase_changed_at": "2026-07-16T00:00:00Z",
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
		t.Fatalf("v4 实际态读取结果不正确: %+v", state)
	}
	if want := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC); !unit.PhaseChangedAt.Equal(want) {
		t.Fatalf("状态时间 = %s，期望 %s", unit.PhaseChangedAt, want)
	}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Version != actualStateVersion || reloaded.Units["backend-main"].Phase != PhaseActive {
		t.Fatalf("生产基线回写后不可重读: %+v", reloaded)
	}
}

func TestDecodeActualStateRejectsUnsupportedVersions(t *testing.T) {
	for _, version := range []int{0, 1, 2, 3, 5, 99} {
		_, err := decodeActualState([]byte(fmt.Sprintf(`{"version":%d,"units":{}}`, version)))
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("版本 %d", version)) {
			t.Fatalf("非生产基线 v%d 实际态必须拒绝，err=%v", version, err)
		}
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
