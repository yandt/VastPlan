package pluginservice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryRetirementRejectsActiveSweepAndResumesPartialRemoval(t *testing.T) {
	body, manifest, err := PackageDirectory(writeTestPlugin(t))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := NewRepository(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := repo.Publish("stable", body)
	if err != nil {
		t.Fatal(err)
	}
	ref := Ref{PluginID: manifest.ID, Version: manifest.Version, Channel: "stable"}
	retirementID := strings.Repeat("a", 64)
	if err := repo.SweepArtifact(ref, artifact.SHA256, retirementID); err == nil {
		t.Fatal("活动制品不得绕过 quarantine 直接 sweep")
	}
	if err := repo.QuarantineArtifact(ref, artifact.SHA256, retirementID); err != nil {
		t.Fatal(err)
	}
	quarantine, err := repo.quarantineDir(ref, retirementID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(quarantine, "artifact.json")); err != nil {
		t.Fatal(err)
	}
	if err := repo.SweepArtifact(ref, artifact.SHA256, retirementID); err != nil {
		t.Fatalf("部分删除后的 sweep 必须可恢复: %v", err)
	}
	if _, err := os.Stat(quarantine); !os.IsNotExist(err) {
		t.Fatalf("隔离目录未清扫: %v", err)
	}
}
