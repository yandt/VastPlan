package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestQuarantineIncompatibleDevelopmentWorkspaceArtifacts(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "artifacts", "cn.vastplan.test.legacy", "0.9.0", "workspace")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := json.RawMessage(`{
  "id":"cn.vastplan.test.legacy","name":"Legacy","description":"Legacy workspace artifact",
  "version":"0.9.0","publisher":"vastplan","engines":{"frontend":"^1.0"},
  "activation":["onPortalStartup"],"entry":{"frontend":"frontend/dist/index.js"},
  "contributes":{"frontend":{"views":[],"menus":[]}}
}`)
	metadata, err := json.Marshal(developmentArtifactMetadataProjection{
		PluginID: "cn.vastplan.test.legacy", Version: "0.9.0", Channel: "workspace", Manifest: manifest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "artifact.json"), metadata, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(root, "catalog")
	if err := os.MkdirAll(filepath.Join(catalog, "journal"), 0o700); err != nil {
		t.Fatal(err)
	}
	index := `{"schemaVersion":"v1","revision":1,"items":[{"ref":{"pluginId":"cn.vastplan.test.legacy","version":"0.9.0","channel":"workspace"}}]}`
	if err := os.WriteFile(filepath.Join(catalog, "index.json"), []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := quarantineIncompatibleDevelopmentWorkspaceArtifacts(root)
	if err != nil || result.Artifacts != 1 || !result.CatalogRebuilt {
		t.Fatalf("旧 workspace 制品和 Catalog 必须一起隔离: result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("活动制品目录仍存在: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(root, "quarantine", "incompatible-manifest", "cn.vastplan.test.legacy", "0.9.0", "*", "artifact.json"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("隔离证据无效: matches=%v err=%v", matches, err)
	}
	archives, err := filepath.Glob(filepath.Join(root, "quarantine", "incompatible-manifest-catalog", "*", "index.json"))
	if err != nil || len(archives) != 1 {
		t.Fatalf("Catalog 审计快照未归档: matches=%v err=%v", archives, err)
	}
	if _, err := os.Stat(filepath.Join(root, "catalog", "journal")); err != nil {
		t.Fatalf("未创建可重建的 Catalog 目录: %v", err)
	}
}
