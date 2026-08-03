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

func TestLoadReleaseSpecDefaultsAndValidatesChangeClass(t *testing.T) {
	path := filepath.Join(t.TempDir(), "release.yaml")
	if err := os.WriteFile(path, []byte("schemaVersion: 1\nmode: development\nplugins:\n  - id: cn.vastplan.demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := LoadReleaseSpec(path)
	if err != nil || spec.Plugins[0].Change != ReleaseChangeImplementation {
		t.Fatalf("default change=%q err=%v", spec.Plugins[0].Change, err)
	}
	if err := os.WriteFile(path, []byte("schemaVersion: 1\nmode: development\nplugins:\n  - id: cn.vastplan.demo\n    change: unknown\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReleaseSpec(path); err == nil {
		t.Fatal("unknown change class must be rejected")
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
