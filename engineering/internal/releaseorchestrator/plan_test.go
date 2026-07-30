package releaseorchestrator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReleaseSpecRejectsProductionDevelopmentTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "release.yaml")
	if err := os.WriteFile(path, []byte("schemaVersion: 1\nmode: production\nplugins:\n  - id: cn.vastplan.demo\n    backendTarget: platform/demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReleaseSpec(path); err == nil {
		t.Fatal("生产计划不得携带开发目标")
	}
}

func TestFoundationCatalogVersionsMatchWorkspace(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := LoadPluginWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := SyncFoundationCatalogVersions(root, workspace, false)
	if err != nil || len(changes) != 0 {
		t.Fatalf("Foundation Catalog 版本未由 Manifest 收敛: changes=%+v err=%v", changes, err)
	}
}

func TestCapabilityContractProjectionsMatchWorkspace(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := LoadPluginWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := SyncCapabilityContractProjections(root, workspace, false)
	if err != nil || len(changes) != 0 {
		t.Fatalf("Capability Contract 投影未由 Manifest 收敛: changes=%+v err=%v", changes, err)
	}
}
