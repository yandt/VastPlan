package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/engineering/internal/plugindev"
)

func TestBackendPluginDevelopmentStatusReadsOnlyValidAtomicSnapshots(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "plugin-dev", "status")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	valid := plugindev.Status{SchemaVersion: 1, PluginID: "cn.vastplan.product.demo", Target: "app/api", Phase: plugindev.PhaseReady, UpdatedAt: time.Now().UTC()}
	raw, _ := json.Marshal(valid)
	if err := os.WriteFile(filepath.Join(directory, "valid.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "broken.json"), []byte(`{"schemaVersion":1`), 0o600); err != nil {
		t.Fatal(err)
	}
	values := backendPluginDevelopmentStatus(root)
	if len(values) != 1 || values[0].PluginID != valid.PluginID || values[0].Phase != plugindev.PhaseReady {
		t.Fatalf("unexpected plugin development status: %+v", values)
	}
}
