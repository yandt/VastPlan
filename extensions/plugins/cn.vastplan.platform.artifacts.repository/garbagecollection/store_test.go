package garbagecollection

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type recoveryStorage struct{ location string }

func (s *recoveryStorage) InspectRetirement(pluginv1.ArtifactRef, string, string) (string, error) {
	return s.location, nil
}
func (s *recoveryStorage) QuarantineArtifact(pluginv1.ArtifactRef, string, string) error {
	s.location = "quarantined"
	return nil
}
func (s *recoveryStorage) SweepArtifact(pluginv1.ArtifactRef, string, string) error {
	s.location = "missing"
	return nil
}

func TestStoreRecoversInterruptedQuarantineAndSweep(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repository")
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	record := Record{
		RetirementID: strings.Repeat("a", 64),
		Ref:          pluginv1.ArtifactRef{PluginID: "cn.example.retired", Version: "1.0.0", Channel: "stable"},
		SHA256:       strings.Repeat("b", 64), Size: 42, Lifecycle: "yanked",
		QuarantinedAt: now, SweepAfter: now.Add(24 * time.Hour),
	}
	if err := store.BeginQuarantine(record); err != nil {
		t.Fatal(err)
	}
	storage := &recoveryStorage{location: "active"}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Recover(storage, now); err != nil {
		t.Fatal(err)
	}
	if state := reopened.List(); storage.location != "quarantined" || state.Items[0].Status != StatusQuarantined {
		t.Fatalf("中断隔离未恢复: storage=%s state=%+v", storage.location, state)
	}
	if err := reopened.BeginSweep(record.Ref, record.SHA256); err != nil {
		t.Fatal(err)
	}
	reopened, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Recover(storage, now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	state := reopened.List()
	if storage.location != "missing" || state.Items[0].Status != StatusSwept || state.Items[0].SweptAt == nil {
		t.Fatalf("中断 sweep 未恢复: storage=%s state=%+v", storage.location, state)
	}
}
